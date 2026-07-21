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
	"github.com/sbengtson/budget/internal/core/config"
	"github.com/sbengtson/budget/internal/core/mail"
	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web/handlers"
)

//go:embed static
var staticFS embed.FS

// Server holds shared state across handlers.
type Server struct {
	store  *store.Store
	engine *gin.Engine
	auth   *auth.Service
}

// NewServer constructs a Gin router wired to the store, building the mailer and
// auth service from config.
func NewServer(s *store.Store, cfg config.Config) *Server {
	gin.SetMode(cfg.Web.Level)
	r := gin.New()
	r.Use(gin.Recovery())

	authSvc := auth.NewService(s, newMailer(cfg), cfg.Web.BaseURL, auth.Config{
		SessionTTL: cfg.Auth.SessionTTL,
		TokenTTL:   cfg.Auth.TokenTTL,
	})

	srv := &Server{store: s, engine: r, auth: authSvc}

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
	hs := handlers.New(s.store, s.auth, cfg.Auth.CookieSecure, int(cfg.Auth.SessionTTL.Seconds()))

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

	// Authenticated app. Everything below requires a valid session.
	app := s.engine.Group("/")
	app.Use(requireAuth(s.auth))

	app.GET("/", func(c *gin.Context) { c.Redirect(http.StatusSeeOther, "/budget") })

	app.GET("/account", hs.AccountPage)
	app.POST("/account/profile", hs.UpdateProfile)
	app.POST("/account/email", hs.RequestEmailChange)
	app.POST("/account/avatar", hs.UpdateAvatar)
	app.POST("/account/avatar/remove", hs.RemoveAvatar)
	app.GET("/account/avatar", hs.ServeAvatar)
	app.POST("/account/password", hs.ChangePassword)

	app.GET("/budget", hs.BudgetIndex)
	app.POST("/budget/assign/:catID", hs.BudgetAssign)
	app.POST("/budget/assign/:catID/copy-prev", hs.BudgetAssignCopyPrev)
	app.POST("/budget/goal/:catID", hs.BudgetGoal)
	app.GET("/budget/goal/:catID/edit", hs.BudgetGoalEdit)
	app.GET("/budget/group/new", hs.BudgetGroupNew)
	app.POST("/budget/group", hs.BudgetGroupCreate)
	app.POST("/budget/groups/reorder", hs.BudgetGroupsReorder)
	app.POST("/budget/categories/reorder", hs.BudgetCategoriesReorder)
	app.PUT("/budget/group/:gid", hs.BudgetGroupRename)
	app.POST("/budget/group/:gid/delete", hs.BudgetGroupDelete)
	app.GET("/budget/group/:gid/category/new", hs.BudgetCategoryNew)
	app.POST("/budget/group/:gid/category", hs.BudgetCategoryCreate)
	app.PUT("/budget/category/:catID", hs.BudgetCategoryRename)
	app.POST("/budget/category/:catID/archive", hs.BudgetCategoryArchive)
	app.POST("/budget/category/:catID/rollover", hs.BudgetSetRollover)
	app.GET("/budget/income", func(c *gin.Context) {
		c.Redirect(http.StatusSeeOther, "/budget")
	})
	app.GET("/budget/income/new", hs.BudgetIncomeNew)
	app.POST("/budget/income", hs.BudgetIncomeCreate)
	app.POST("/budget/income/copy-prev", hs.BudgetIncomeCopyPrev)
	app.PUT("/budget/income/:id", hs.BudgetIncomeUpdate)
	app.DELETE("/budget/income/:id", hs.BudgetIncomeDelete)

	app.GET("/transactions", hs.TransactionsIndex)
	app.GET("/transactions/new", hs.TransactionsNew)
	app.POST("/transactions", hs.TransactionsCreate)
	app.GET("/transactions/:id/edit", hs.TransactionsEdit)
	app.PUT("/transactions/:id", hs.TransactionsUpdate)
	app.DELETE("/transactions/:id", hs.TransactionsDelete)
	app.POST("/transactions/:id/cleared", hs.TransactionsToggleCleared)

	app.GET("/accounts/overview", hs.AccountsOverviewPartial)
	app.GET("/accounts/new", hs.AccountsNew)
	app.POST("/accounts", hs.AccountsCreate)
	app.GET("/accounts/:id/edit", hs.AccountsEdit)
	app.PUT("/accounts/:id", hs.AccountsUpdate)
	app.POST("/accounts/:id/archive", hs.AccountsArchive)

	// Category management now lives inline on the Budget page; the standalone
	// Categories page was removed. Redirect any old links there.
	app.GET("/categories", func(c *gin.Context) {
		c.Redirect(http.StatusSeeOther, "/budget")
	})

	app.GET("/paydown", hs.PaydownIndex)
	app.GET("/paydown/:acctID/rows", hs.PaydownRows)
	app.POST("/paydown/:acctID/include", hs.PaydownInclude)
	app.POST("/paydown/:acctID/exclude", hs.PaydownExclude)
	app.GET("/paydown/:acctID/payment-form", hs.PaydownPaymentForm)
	app.GET("/paydown/:acctID/category-form", hs.PaydownCategoryForm)
	app.POST("/paydown/:acctID/payment", hs.PaydownSetPayment)
	app.POST("/paydown/:acctID/category", hs.PaydownSetCategory)
}
