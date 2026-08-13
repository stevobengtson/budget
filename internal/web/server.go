// Package web hosts the HTMX + Gin + Templ frontend.
package web

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
	templuicomponents "github.com/templui/templui/components"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/billing"
	"github.com/sbengtson/budget/internal/core/config"
	"github.com/sbengtson/budget/internal/core/i18n"
	"github.com/sbengtson/budget/internal/core/mail"
	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web/apiv1"
	"github.com/sbengtson/budget/internal/web/handlers"
	"github.com/sbengtson/budget/internal/web/views"
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
	// requestLogger is outermost so it observes the final status (even a
	// Recovery-produced 500). sentrygin sits just inside Recovery with
	// Repanic:true: it captures the panic to Sentry, then re-panics so
	// gin.Recovery writes the 500 response. Wired only when a DSN is set.
	r.Use(requestLogger())
	r.Use(gin.Recovery())
	if cfg.Sentry.DSN != "" {
		r.Use(sentrygin.New(sentrygin.Options{Repanic: true}))
	}
	// Every request carries a locale from here on, so no renderer has to cope
	// with its absence. requireAuth later replaces it with the signed-in user's
	// stored preference.
	r.Use(resolveLocale())

	authSvc := auth.NewService(s, newMailer(cfg), cfg.Web.BaseURL, auth.Config{
		SessionTTL: cfg.Auth.SessionTTL,
		TokenTTL:   cfg.Auth.TokenTTL,
	})
	billingSvc := billing.NewService(s, cfg.Stripe.SecretKey, cfg.Stripe.PriceIDs.Base, cfg.Web.BaseURL, cfg.Stripe.WebhookSecret)

	srv := &Server{store: s, engine: r, auth: authSvc, billing: billingSvc}

	staticSub, _ := fs.Sub(staticFS, "static")
	views.SetAssetHashes(assetHashes(staticSub))
	// Cache hard, but only for a request that names a version. A versioned URL
	// can never go stale — its content hash changes with the bytes — while a
	// bare /static/x request might be anything, so it revalidates.
	r.GET("/static/*filepath", func(c *gin.Context) {
		if c.Query("v") != "" {
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			c.Header("Cache-Control", "no-cache")
		}
		http.FileServer(http.FS(staticSub)).ServeHTTP(c.Writer, requestWithPath(c.Request, c.Param("filepath")))
	})

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

// requestWithPath rewrites a request's URL path, so the /static/*filepath route
// can hand the wildcard remainder to http.FileServer, which serves relative to
// its own FS root.
func requestWithPath(r *http.Request, p string) *http.Request {
	r2 := r.Clone(r.Context())
	r2.URL.Path = p
	return r2
}

// assetHashes walks the embedded static files once and returns a map of request
// path to a short content hash, e.g. "/static/app.css" -> "1f3c9a2b". Views
// append it as ?v= so a changed file busts the browser cache and an unchanged
// one keeps it (see views.Asset).
//
// Hashing at startup rather than at build time keeps it honest with whatever is
// actually embedded — there is no manifest to regenerate and forget.
func assetHashes(dir fs.FS) map[string]string {
	out := map[string]string{}
	err := fs.WalkDir(dir, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(dir, p)
		if err != nil {
			// A file we cannot read simply goes unversioned; it is still served.
			slog.Warn("asset hash: read failed", "path", p, "err", err)
			return nil
		}
		sum := sha256.Sum256(b)
		out["/static/"+p] = hex.EncodeToString(sum[:])[:8]
		return nil
	})
	if err != nil {
		slog.Warn("asset hash: walk failed", "err", err)
	}
	return out
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

// healthz is a readiness probe for uptime monitoring: it pings Postgres and
// reports 200 only when the DB is reachable, 503 otherwise. The body carries no
// error detail (it's public); the reason is logged server-side.
func (s *Server) healthz(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.DB().PingContext(ctx); err != nil {
		slog.Error("healthz: db ping failed", "err", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "degraded"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// newMailer builds the configured mail driver: resend in production, console
// (logs the link) for local dev.
func newMailer(cfg config.Config) mail.Mailer {
	if cfg.Mail.Driver == "resend" {
		return mail.NewResend(cfg.Mail.ResendAPIKey, cfg.Mail.From)
	}
	return mail.NewConsole()
}

func (s *Server) routes(cfg config.Config) {
	hs := handlers.New(s.store, s.auth, s.billing, cfg.Auth.CookieSecure, int(cfg.Auth.SessionTTL.Seconds()), cfg.Web.BaseURL)

	// Ops readiness probe: pings the DB so an uptime monitor learns of a
	// Postgres outage, not just a live HTTP process. (The mobile app's
	// /api/v1/health stays a pure liveness check by design.)
	s.engine.GET("/healthz", s.healthz)

	// Public marketing + legal pages, served at one URL per language: the English
	// set at the root and the French set under /fr. The locale comes from the URL
	// rather than the visitor's cookie, because these are the pages that have to
	// be indexable and linkable per language — a shared link must open in the
	// language it was read in, and a crawler must find both.
	//
	// The app itself stays cookie/preference-driven; only these five pages are
	// mirrored. Logged-in visitors to "/" are redirected into the app by Home.
	//
	// Registration is driven by views.PublicPages, the same list the sitemap
	// enumerates, so a page cannot end up routed but unlisted (or the reverse).
	publicHandlers := map[string]gin.HandlerFunc{
		"/":        hs.Home,
		"/privacy": hs.PrivacyPage,
		"/terms":   hs.TermsPage,
		"/refund":  hs.RefundPage,
		"/contact": hs.ContactPage,
	}
	for _, locale := range i18n.Supported {
		g := s.engine.Group(publicPrefix(locale))
		g.Use(pinLocale(locale))
		for _, page := range views.PublicPages {
			h, ok := publicHandlers[page]
			if !ok {
				// Startup, not a 404 on some later request: a page listed for the
				// sitemap with nothing to serve it is a build mistake.
				panic("web: no handler for public page " + page)
			}
			// An empty relative path resolves to the group's own base path, which
			// is "/" for the root group and "/fr" for the French one. Passing the
			// prefix here instead would register "/fr/fr".
			rel := page
			if rel == "/" {
				rel = ""
			}
			g.GET(rel, h)
		}
	}

	// The sitemap lists every language's URL for every public page. Not itself
	// language-scoped, so it sits outside those groups. robots.txt points at it,
	// which is how a crawler finds the sitemap without it being submitted by hand.
	s.engine.GET("/sitemap.xml", hs.Sitemap)
	s.engine.GET("/robots.txt", hs.Robots)

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
	// Public: the language has to be switchable on the login and sign-up pages,
	// before there is a session to attach the choice to. Persists to the user
	// record as well when the request does carry one.
	s.engine.POST("/locale", hs.SetLocale)

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
	app.GET("/account/billing", hs.AccountBillingSection)
	app.POST("/account/profile", hs.UpdateProfile)
	app.POST("/account/email", hs.RequestEmailChange)
	app.POST("/account/avatar", hs.UpdateAvatar)
	app.POST("/account/avatar/remove", hs.RemoveAvatar)
	app.GET("/account/avatar", hs.ServeAvatar)
	app.POST("/account/password", hs.ChangePassword)
	app.POST("/account/addons/:slug", hs.ToggleAddOn)
	// Danger zone: wipe all budget data (keep account) / delete the account.
	app.POST("/account/start-fresh", hs.StartFresh)
	app.POST("/account/delete", hs.DeleteAccount)

	// Admin console. Authenticated but deliberately NOT behind the subscription
	// gate: an operator has to be able to reach it while their own billing is in
	// whatever state, and the console is where lapsed accounts get sorted out.
	admin := app.Group("/admin")
	admin.Use(requireAdmin())
	admin.GET("", hs.AdminDashboard)
	admin.GET("/chart", hs.AdminSignupsChart)
	admin.GET("/users", hs.AdminUsers)
	admin.GET("/users/:id", hs.AdminUserDetail)
	admin.POST("/users/:id/disable", hs.AdminUserDisable)
	admin.POST("/users/:id/enable", hs.AdminUserEnable)
	admin.POST("/users/:id/comp", hs.AdminUserGrantComp)
	admin.POST("/users/:id/comp/revoke", hs.AdminUserRevokeComp)
	admin.POST("/users/:id/delete", hs.AdminUserDelete)

	app.GET("/billing", hs.BillingPage)
	app.POST("/billing/checkout", hs.StartCheckout)
	// /billing/start is the gate's auto-redirect target for never-subscribed
	// users: it opens the free-trial Checkout (same handler as the button). GET so
	// a plain redirect reaches it. Ungated, or the gate would loop onto it.
	app.GET("/billing/start", hs.StartCheckout)
	app.POST("/billing/portal", hs.OpenPortal)
	app.GET("/billing/success", hs.CheckoutSuccess)

	// The account-overview widget. It used to load on every page from the shared
	// sidebar, including /billing and /account which a locked-out user sees, so
	// it had to stay ungated or its on-load hx-get would redirect to /billing and
	// loop the page reload. The redesign mounts it only on /budget and
	// /transactions, both gated, so that rationale is now historical — it stays
	// here because it is a read-only view of the user's own accounts and moving
	// it buys nothing. Worth gating once Phase 2/3 settle its final home.
	app.GET("/accounts/overview", hs.AccountsOverviewPartial)

	// Core app: gated behind an active trial or paid subscription. A user without
	// access is redirected to /billing. The gate is a no-op when billing isn't
	// configured (empty base price), so dev without Stripe still works.
	subscribed := app.Group("")
	subscribed.Use(requireSubscription(s.store, s.billing))

	// The first-run wizard. Subscription-gated like the rest of the app, but
	// deliberately outside requireOnboarded — it is what clears that gate.
	subscribed.GET("/welcome", hs.WelcomeStart)
	subscribed.POST("/welcome/language", hs.WelcomeLanguage)
	subscribed.POST("/welcome/categories", hs.WelcomeCategories)
	subscribed.POST("/welcome/back", hs.WelcomeBack)
	subscribed.POST("/welcome/finish", hs.WelcomeFinish)

	// Everything past here additionally requires the wizard to be done, so no
	// screen has to cope with a user who has no categories yet.
	gated := subscribed.Group("")
	gated.Use(requireOnboarded())

	gated.GET("/budget", hs.BudgetIndex)
	gated.POST("/budget/assign/:catID", hs.BudgetAssign)
	gated.GET("/budget/assign/:catID/sheet", hs.BudgetAssignSheet)
	gated.POST("/budget/assign/:catID/copy-prev", hs.BudgetAssignCopyPrev)
	gated.POST("/budget/goal/:catID", hs.BudgetGoal)
	gated.GET("/budget/goal/:catID/edit", hs.BudgetGoalEdit)
	gated.POST("/budget/goal/:catID/preview", hs.BudgetGoalPreview)
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
	gated.POST("/budget/category/:catID/unarchive", hs.BudgetCategoryUnarchive)
	gated.POST("/budget/category/:catID/rollover", hs.BudgetSetRollover)
	gated.GET("/budget/archived/panel", hs.BudgetArchivedPanel)
	gated.GET("/budget/income", func(c *gin.Context) {
		c.Redirect(http.StatusSeeOther, "/budget")
	})
	gated.GET("/budget/income/panel", hs.BudgetIncomePanel)
	gated.GET("/budget/credit/panel", hs.BudgetCreditPanel)
	gated.GET("/budget/income/new", hs.BudgetIncomeNew)
	gated.POST("/budget/income", hs.BudgetIncomeCreate)
	gated.POST("/budget/income/copy-prev", hs.BudgetIncomeCopyPrev)
	gated.PUT("/budget/income/:id", hs.BudgetIncomeUpdate)
	gated.DELETE("/budget/income/:id", hs.BudgetIncomeDelete)

	gated.GET("/transactions", hs.TransactionsIndex)
	gated.POST("/transactions", hs.TransactionsCreate)
	gated.GET("/transactions/:id/edit-row", hs.TransactionsEditRow)
	gated.GET("/transactions/:id/row", hs.TransactionsRow)
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

	// Budget Estimate is likewise an opt-in add-on: the whole group sits behind
	// its enabled flag, so a disabled user hitting any /estimates URL directly is
	// redirected to /budget.
	estimates := gated.Group("/estimates")
	estimates.Use(requireAddOn("estimates"))
	estimates.GET("", hs.EstimatesIndex)
	estimates.POST("", hs.EstimatesCreate)
	estimates.GET("/:id", hs.EstimateShow)
	estimates.POST("/:id/delete", hs.EstimateDelete)
	estimates.GET("/:id/income/new", hs.EstimateIncomeNew)
	estimates.POST("/:id/income", hs.EstimateIncomeCreate)
	estimates.PUT("/:id/income/:iid", hs.EstimateIncomeUpdate)
	estimates.DELETE("/:id/income/:iid", hs.EstimateIncomeDelete)
	estimates.GET("/:id/group/new", hs.EstimateGroupNew)
	estimates.POST("/:id/group", hs.EstimateGroupCreate)
	estimates.PUT("/:id/group/:gid", hs.EstimateGroupRename)
	estimates.POST("/:id/group/:gid/delete", hs.EstimateGroupDelete)
	estimates.POST("/:id/groups/reorder", hs.EstimateGroupsReorder)
	estimates.GET("/:id/group/:gid/category/new", hs.EstimateCategoryNew)
	estimates.POST("/:id/group/:gid/category", hs.EstimateCategoryCreate)
	estimates.PUT("/:id/category/:catID", hs.EstimateCategoryRename)
	estimates.POST("/:id/category/:catID/assign", hs.EstimateAssign)
	estimates.DELETE("/:id/category/:catID", hs.EstimateCategoryDelete)
	estimates.POST("/:id/categories/reorder", hs.EstimateCategoriesReorder)
}

// publicPrefix is the URL prefix a locale's public pages live under: none for
// the default language, /<base-language> for the rest. Mirrors views.PublicHref,
// which builds the same URLs for links and the sitemap.
func publicPrefix(l i18n.Locale) string {
	if l == i18n.Default {
		return ""
	}
	base, _, _ := strings.Cut(string(l), "-")
	return "/" + base
}
