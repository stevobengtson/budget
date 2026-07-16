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

	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web/handlers"
)

//go:embed static
var staticFS embed.FS

// Server holds shared state across handlers.
type Server struct {
	store  *store.Store
	engine *gin.Engine
}

// NewServer constructs a Gin router wired to the store.
func NewServer(s *store.Store) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	srv := &Server{store: s, engine: r}

	staticSub, _ := fs.Sub(staticFS, "static")
	r.StaticFS("/static", http.FS(staticSub))

	// templUI component scripts (input/checkbox/dialog/label/popover/selectbox)
	// are referenced by each component's Script() as /templui/js/<name>.min.js.
	// The templui module embeds these files in components.TemplFiles; serve them
	// from there (mirrors utils.SetupScriptRoutes, which only supports the stdlib
	// http.ServeMux, not Gin). Without this route selectbox.js never loads and the
	// JS-driven selectboxes are inert.
	r.GET("/templui/js/:file", serveTemplUIScript)

	srv.routes()
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

func (s *Server) routes() {
	s.engine.GET("/", func(c *gin.Context) { c.Redirect(http.StatusSeeOther, "/budget") })

	hs := handlers.New(s.store)

	s.engine.GET("/budget", hs.BudgetIndex)
	s.engine.POST("/budget/assign/:catID", hs.BudgetAssign)
	s.engine.POST("/budget/assign/:catID/copy-prev", hs.BudgetAssignCopyPrev)
	s.engine.POST("/budget/goal/:catID", hs.BudgetGoal)
	s.engine.GET("/budget/goal/:catID/edit", hs.BudgetGoalEdit)
	s.engine.GET("/budget/group/new", hs.BudgetGroupNew)
	s.engine.POST("/budget/group", hs.BudgetGroupCreate)
	s.engine.PUT("/budget/group/:gid", hs.BudgetGroupRename)
	s.engine.POST("/budget/group/:gid/delete", hs.BudgetGroupDelete)
	s.engine.GET("/budget/group/:gid/category/new", hs.BudgetCategoryNew)
	s.engine.POST("/budget/group/:gid/category", hs.BudgetCategoryCreate)
	s.engine.PUT("/budget/category/:catID", hs.BudgetCategoryRename)
	s.engine.POST("/budget/category/:catID/archive", hs.BudgetCategoryArchive)
	s.engine.POST("/budget/category/:catID/rollover", hs.BudgetSetRollover)
	s.engine.GET("/budget/income", func(c *gin.Context) {
		c.Redirect(http.StatusSeeOther, "/budget")
	})
	s.engine.GET("/budget/income/new", hs.BudgetIncomeNew)
	s.engine.POST("/budget/income", hs.BudgetIncomeCreate)
	s.engine.POST("/budget/income/copy-prev", hs.BudgetIncomeCopyPrev)
	s.engine.PUT("/budget/income/:id", hs.BudgetIncomeUpdate)
	s.engine.DELETE("/budget/income/:id", hs.BudgetIncomeDelete)

	s.engine.GET("/transactions", hs.TransactionsIndex)
	s.engine.GET("/transactions/new", hs.TransactionsNew)
	s.engine.POST("/transactions", hs.TransactionsCreate)
	s.engine.GET("/transactions/:id/edit", hs.TransactionsEdit)
	s.engine.PUT("/transactions/:id", hs.TransactionsUpdate)
	s.engine.DELETE("/transactions/:id", hs.TransactionsDelete)
	s.engine.POST("/transactions/:id/cleared", hs.TransactionsToggleCleared)

	s.engine.GET("/accounts/overview", hs.AccountsOverviewPartial)
	s.engine.GET("/accounts/new", hs.AccountsNew)
	s.engine.POST("/accounts", hs.AccountsCreate)
	s.engine.GET("/accounts/:id/edit", hs.AccountsEdit)
	s.engine.PUT("/accounts/:id", hs.AccountsUpdate)
	s.engine.POST("/accounts/:id/archive", hs.AccountsArchive)

	// Category management now lives inline on the Budget page; the standalone
	// Categories page was removed. Redirect any old links there.
	s.engine.GET("/categories", func(c *gin.Context) {
		c.Redirect(http.StatusSeeOther, "/budget")
	})

	s.engine.GET("/paydown", hs.PaydownIndex)
	s.engine.GET("/paydown/:acctID/rows", hs.PaydownRows)
	s.engine.POST("/paydown/:acctID/include", hs.PaydownInclude)
	s.engine.POST("/paydown/:acctID/exclude", hs.PaydownExclude)
	s.engine.GET("/paydown/:acctID/payment-form", hs.PaydownPaymentForm)
	s.engine.GET("/paydown/:acctID/category-form", hs.PaydownCategoryForm)
	s.engine.POST("/paydown/:acctID/payment", hs.PaydownSetPayment)
	s.engine.POST("/paydown/:acctID/category", hs.PaydownSetCategory)
}
