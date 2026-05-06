// Package web hosts the HTMX + Gin + Templ frontend.
package web

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/store"
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

	srv.routes()
	return srv
}

func (s *Server) Handler() http.Handler { return s.engine }

func (s *Server) routes() {
	s.engine.GET("/", func(c *gin.Context) { c.Redirect(http.StatusSeeOther, "/budget") })

	hs := handlers.New(s.store)

	s.engine.GET("/budget", hs.BudgetIndex)
	s.engine.POST("/budget/assign/:catID", hs.BudgetAssign)
	s.engine.POST("/budget/goal/:catID", hs.BudgetGoal)
	s.engine.GET("/budget/income", hs.BudgetIncomePanel)
	s.engine.GET("/budget/income/new", hs.BudgetIncomeNew)
	s.engine.GET("/budget/income/:id/edit", hs.BudgetIncomeEdit)
	s.engine.POST("/budget/income", hs.BudgetIncomeCreate)
	s.engine.PUT("/budget/income/:id", hs.BudgetIncomeUpdate)
	s.engine.DELETE("/budget/income/:id", hs.BudgetIncomeDelete)

	s.engine.GET("/transactions", hs.TransactionsIndex)
	s.engine.GET("/transactions/new", hs.TransactionsNew)
	s.engine.POST("/transactions", hs.TransactionsCreate)
	s.engine.GET("/transactions/:id/edit", hs.TransactionsEdit)
	s.engine.PUT("/transactions/:id", hs.TransactionsUpdate)
	s.engine.DELETE("/transactions/:id", hs.TransactionsDelete)
	s.engine.POST("/transactions/:id/cleared", hs.TransactionsToggleCleared)

	s.engine.GET("/accounts", hs.AccountsIndex)
	s.engine.GET("/accounts/new", hs.AccountsNew)
	s.engine.POST("/accounts", hs.AccountsCreate)
	s.engine.GET("/accounts/:id/edit", hs.AccountsEdit)
	s.engine.PUT("/accounts/:id", hs.AccountsUpdate)
	s.engine.POST("/accounts/:id/archive", hs.AccountsArchive)

	s.engine.GET("/categories", hs.CategoriesIndex)
	s.engine.POST("/categories/group", hs.CategoriesCreateGroup)
	s.engine.POST("/categories", hs.CategoriesCreate)
	s.engine.PUT("/categories/:id", hs.CategoriesUpdate)
	s.engine.POST("/categories/:id/archive", hs.CategoriesArchive)

	s.engine.GET("/paydown", hs.PaydownIndex)
	s.engine.POST("/paydown/:acctID/include", hs.PaydownInclude)
	s.engine.POST("/paydown/:acctID/exclude", hs.PaydownExclude)
	s.engine.GET("/paydown/:acctID/payment-form", hs.PaydownPaymentForm)
	s.engine.GET("/paydown/:acctID/category-form", hs.PaydownCategoryForm)
	s.engine.POST("/paydown/:acctID/payment", hs.PaydownSetPayment)
	s.engine.POST("/paydown/:acctID/category", hs.PaydownSetCategory)

	s.engine.GET("/reports", func(c *gin.Context) { c.Redirect(http.StatusSeeOther, "/reports/spending") })
	s.engine.GET("/reports/spending", hs.ReportsSpending)
	s.engine.GET("/reports/cashflow", hs.ReportsCashflow)
}
