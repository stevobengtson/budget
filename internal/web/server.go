// Package web hosts the HTMX + Gin + Templ frontend.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
	templuicomponents "github.com/templui/templui/components"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/billing"
	"github.com/sbengtson/budget/internal/core/config"
	"github.com/sbengtson/budget/internal/core/mail"
	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web/apiv1"
	"github.com/sbengtson/budget/internal/web/handlers"
)

//go:embed static
var staticFS embed.FS

// Server holds shared state across handlers.
type Server struct {
	store   *store.Store
	engine  *gin.Engine
	auth    *auth.Service
	billing *billing.Service
}

// NewServer constructs a Gin router wired to the store, building the mailer,
// auth service, and billing (Stripe) service from config.
func NewServer(s *store.Store, cfg config.Config) *Server {
	gin.SetMode(cfg.Web.Level)
	r := gin.New()
	r.Use(gin.Recovery())

	authSvc := auth.NewService(s, newMailer(cfg), cfg.Web.BaseURL, auth.Config{
		SessionTTL: cfg.Auth.SessionTTL,
		TokenTTL:   cfg.Auth.TokenTTL,
	})
	billingSvc := billing.NewService(s, cfg.Stripe.SecretKey, cfg.Stripe.PriceIDs.Base, cfg.Web.BaseURL, cfg.Stripe.WebhookSecret)

	srv := &Server{store: s, engine: r, auth: authSvc, billing: billingSvc}

	staticSub, _ := fs.Sub(staticFS, "static")
	r.StaticFS("/static", http.FS(staticSub))

	// templUI component scripts (input/checkbox/dialog/label/popover/selectbox)
	// are referenced by each component's Script() as /templui/js/<name>.min.js.
	// The templui module embeds these files in components.TemplFiles; serve them
	// from there (mirrors utils.SetupScriptRoutes, which only supports the stdlib
	// http.ServeMux, not Gin). Without this route selectbox.js never loads and the
	// JS-driven selectboxes are inert.
	r.GET("/templui/js/:file", serveTemplUIScript)

	srv.routes(cfg)
	return srv
}

// serveTemplUIScript serves a templUI component's embedded JavaScript by file
// name (e.g. "selectbox.min.js"), reading it from components.TemplFiles under
// its component directory (e.g. "selectbox/selectbox.min.js").
func serveTemplUIScript(c *gin.Context) {
	file := c.Param("file")
	if file == "" || strings.Contains(file, "..") || strings.Contains(file, "/") {
		c.Status(http.StatusNotFound)
		return
	}
	component := strings.TrimSuffix(strings.TrimSuffix(file, ".min.js"), ".js")
	data, err := templuicomponents.TemplFiles.ReadFile(path.Join(component, file))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "public, max-age=31536000")
	c.Data(http.StatusOK, "application/javascript; charset=utf-8", data)
}

func (s *Server) Handler() http.Handler { return s.engine }

// newMailer builds the configured mail driver: resend in production, console
// (logs the link) for local dev.
func newMailer(cfg config.Config) mail.Mailer {
	if cfg.Mail.Driver == "resend" {
		return mail.NewResend(cfg.Mail.ResendAPIKey, cfg.Mail.From)
	}
	return mail.NewConsole()
}

func (s *Server) routes(cfg config.Config) {
	hs := handlers.New(s.store, s.auth, s.billing, cfg.Auth.CookieSecure, int(cfg.Auth.SessionTTL.Seconds()))

	// Public marketing landing page. Logged-in visitors are redirected into the
	// app by the handler; everyone else sees the landing page.
	s.engine.GET("/", hs.Home)

	// Public informational / legal pages, linked from the marketing footer.
	s.engine.GET("/contact", hs.ContactPage)
	s.engine.GET("/privacy", hs.PrivacyPage)
	s.engine.GET("/refund", hs.RefundPage)
	s.engine.GET("/terms", hs.TermsPage)

	// Public auth routes (no session required).
	s.engine.GET("/login", hs.LoginForm)
	s.engine.POST("/login", hs.Login)
	s.engine.GET("/signup", hs.SignupForm)
	s.engine.POST("/signup", hs.Signup)
	s.engine.GET("/verify", hs.Verify)
	// Public: the email-change confirmation link is clicked from the new address's
	// inbox, possibly while logged out, so the token (not a session) authorizes it.
	s.engine.GET("/account/email/verify", hs.ConfirmEmailChange)
	s.engine.GET("/forgot", hs.ForgotForm)
	s.engine.POST("/forgot", hs.Forgot)
	s.engine.GET("/reset", hs.ResetForm)
	s.engine.POST("/reset", hs.Reset)
	s.engine.POST("/logout", hs.Logout)

	// Stripe webhooks: public (no session), authenticated by the Stripe signature.
	s.engine.POST("/webhooks/stripe", hs.StripeWebhook)

	// JSON API for the native mobile apps. Separate group with its own bearer-
	// token auth; renders no HTML and reuses the same store/auth/billing services.
	apiv1.New(s.store, s.auth, s.billing).Register(s.engine.Group("/api/v1"))

	// Authenticated app. Everything below requires a valid session.
	app := s.engine.Group("/")
	app.Use(requireAuth(s.auth, s.store))

	// Account + billing stay reachable even when a subscription has lapsed, so a
	// locked-out user can still manage their account and subscribe.
	app.GET("/account", hs.AccountPage)
	app.POST("/account/profile", hs.UpdateProfile)
	app.POST("/account/email", hs.RequestEmailChange)
	app.POST("/account/avatar", hs.UpdateAvatar)
	app.POST("/account/avatar/remove", hs.RemoveAvatar)
	app.GET("/account/avatar", hs.ServeAvatar)
	app.POST("/account/password", hs.ChangePassword)
	app.POST("/account/addons/:slug", hs.ToggleAddOn)

	app.GET("/billing", hs.BillingPage)
	app.POST("/billing/checkout", hs.StartCheckout)
	// /billing/start is the gate's auto-redirect target for never-subscribed
	// users: it opens the free-trial Checkout (same handler as the button). GET so
	// a plain redirect reaches it. Ungated, or the gate would loop onto it.
	app.GET("/billing/start", hs.StartCheckout)
	app.POST("/billing/portal", hs.OpenPortal)
	app.GET("/billing/success", hs.CheckoutSuccess)

	// The sidebar account-overview widget loads on every page (including /billing
	// and /account, which a locked-out user sees). It's a read-only view of the
	// user's own accounts, so it stays ungated — gating it would make the shared
	// sidebar's on-load hx-get redirect to /billing, looping the page reload.
	app.GET("/accounts/overview", hs.AccountsOverviewPartial)

	// Core app: gated behind an active trial or paid subscription. A user without
	// access is redirected to /billing. The gate is a no-op when billing isn't
	// configured (empty base price), so dev without Stripe still works.
	gated := app.Group("")
	gated.Use(requireSubscription(s.store, s.billing))

	gated.GET("/budget", hs.BudgetIndex)
	gated.POST("/budget/assign/:catID", hs.BudgetAssign)
	gated.POST("/budget/assign/:catID/copy-prev", hs.BudgetAssignCopyPrev)
	gated.POST("/budget/goal/:catID", hs.BudgetGoal)
	gated.GET("/budget/goal/:catID/edit", hs.BudgetGoalEdit)
	gated.GET("/budget/group/new", hs.BudgetGroupNew)
	gated.POST("/budget/group", hs.BudgetGroupCreate)
	gated.POST("/budget/groups/reorder", hs.BudgetGroupsReorder)
	gated.POST("/budget/categories/reorder", hs.BudgetCategoriesReorder)
	gated.PUT("/budget/group/:gid", hs.BudgetGroupRename)
	gated.POST("/budget/group/:gid/delete", hs.BudgetGroupDelete)
	gated.GET("/budget/group/:gid/category/new", hs.BudgetCategoryNew)
	gated.POST("/budget/group/:gid/category", hs.BudgetCategoryCreate)
	gated.PUT("/budget/category/:catID", hs.BudgetCategoryRename)
	gated.POST("/budget/category/:catID/archive", hs.BudgetCategoryArchive)
	gated.POST("/budget/category/:catID/rollover", hs.BudgetSetRollover)
	gated.GET("/budget/income", func(c *gin.Context) {
		c.Redirect(http.StatusSeeOther, "/budget")
	})
	gated.GET("/budget/income/new", hs.BudgetIncomeNew)
	gated.POST("/budget/income", hs.BudgetIncomeCreate)
	gated.POST("/budget/income/copy-prev", hs.BudgetIncomeCopyPrev)
	gated.PUT("/budget/income/:id", hs.BudgetIncomeUpdate)
	gated.DELETE("/budget/income/:id", hs.BudgetIncomeDelete)

	gated.GET("/transactions", hs.TransactionsIndex)
	gated.GET("/transactions/new", hs.TransactionsNew)
	gated.POST("/transactions", hs.TransactionsCreate)
	gated.GET("/transactions/:id/edit", hs.TransactionsEdit)
	gated.PUT("/transactions/:id", hs.TransactionsUpdate)
	gated.DELETE("/transactions/:id", hs.TransactionsDelete)
	gated.POST("/transactions/:id/cleared", hs.TransactionsToggleCleared)

	gated.GET("/accounts/new", hs.AccountsNew)
	gated.POST("/accounts", hs.AccountsCreate)
	gated.GET("/accounts/:id/edit", hs.AccountsEdit)
	gated.PUT("/accounts/:id", hs.AccountsUpdate)
	gated.POST("/accounts/:id/archive", hs.AccountsArchive)

	// Category management now lives inline on the Budget page; the standalone
	// Categories page was removed. Redirect any old links there.
	gated.GET("/categories", func(c *gin.Context) {
		c.Redirect(http.StatusSeeOther, "/budget")
	})

	// Paydown is an opt-in add-on: gate the whole route group behind it so a
	// disabled user hitting any /paydown URL directly is redirected to /budget.
	paydown := gated.Group("/paydown")
	paydown.Use(requireAddOn("paydown"))
	paydown.GET("", hs.PaydownIndex)
	paydown.GET("/:acctID/rows", hs.PaydownRows)
	paydown.POST("/:acctID/include", hs.PaydownInclude)
	paydown.POST("/:acctID/exclude", hs.PaydownExclude)
	paydown.GET("/:acctID/payment-form", hs.PaydownPaymentForm)
	paydown.GET("/:acctID/category-form", hs.PaydownCategoryForm)
	paydown.POST("/:acctID/payment", hs.PaydownSetPayment)
	paydown.POST("/:acctID/category", hs.PaydownSetCategory)
}
