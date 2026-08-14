package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/mail"
	"github.com/sbengtson/budget/internal/core/store"
)

func TestRequireAuthRedirectsAnonymous(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st := store.New(openTestDB(t))
	svc := auth.NewService(st, mail.NewConsole(), "http://x", auth.Config{})

	r := gin.New()
	r.GET("/p", requireAuth(svc, st, false), func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/p", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Fatalf("want redirect to /login, got %d %q", w.Code, w.Header().Get("Location"))
	}
}
