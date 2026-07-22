package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/billing"
	"github.com/sbengtson/budget/internal/core/db"
	"github.com/sbengtson/budget/internal/core/mail"
	"github.com/sbengtson/budget/internal/core/store"
)

// testDBLockKey MUST match the store/db/web packages' advisory-lock key so all
// DB-backed tests serialize on the shared budget_test database.
const testDBLockKey = 918273645

func testDSN() string {
	if u := os.Getenv("BUDGET_POSTGRES_URL"); u != "" {
		return u
	}
	return "postgres://postgres:postgres@127.0.0.1:5432/budget_test?sslmode=disable"
}

var testTables = []string{
	"transactions", "budgets", "categories", "category_groups",
	"incomes", "accounts", "verification_tokens", "sessions", "users",
}

// openTestDB opens the shared Postgres test database under a global advisory
// lock, migrates, truncates, and re-seeds the global Income group/category.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	lockConn, _, err := db.Open(testDSN(), false)
	if err != nil {
		t.Fatalf("open lock conn: %v", err)
	}
	lockConn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = lockConn.Close() })
	if _, err := lockConn.Exec("SELECT pg_advisory_lock($1)", testDBLockKey); err != nil {
		t.Fatalf("advisory lock: %v", err)
	}

	conn, dialect, err := db.Open(testDSN(), false)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := db.MigrateUp(conn, dialect); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := conn.Exec("TRUNCATE TABLE " + strings.Join(testTables, ", ") + " RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	var gid int64
	if err := conn.QueryRow(
		`INSERT INTO category_groups(name, sort_order) VALUES ('Income', -100) RETURNING id`).Scan(&gid); err != nil {
		t.Fatalf("seed income group: %v", err)
	}
	if _, err := conn.Exec(
		`INSERT INTO categories(group_id, name, is_income, sort_order) VALUES ($1, 'Income', TRUE, 0)`, gid); err != nil {
		t.Fatalf("seed income category: %v", err)
	}
	return conn
}

func newTestHandlers(t *testing.T) *Handlers {
	t.Helper()
	st := store.New(openTestDB(t))
	svc := auth.NewService(st, mail.NewConsole(), "http://localhost:8080", auth.Config{})
	bill := billing.NewService(st, "", "", "http://localhost:8080", "")
	return New(st, svc, bill, false, 3600)
}

func postForm(t *testing.T, r http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)
	return w
}

func TestSignupThenLoginBlockedUntilVerified(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestHandlers(t)
	r := gin.New()
	r.POST("/signup", h.Signup)
	r.POST("/login", h.Login)

	form := url.Values{"email": {"x@example.com"}, "password": {"password1"}}

	w := postForm(t, r, "/signup", form)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Check your email") {
		t.Fatalf("signup: %d %s", w.Code, w.Body.String())
	}

	// Login before verification must be rejected.
	w = postForm(t, r, "/login", form)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("login should be blocked until verified: %d", w.Code)
	}
}

func TestSignupRejectsShortPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestHandlers(t)
	r := gin.New()
	r.POST("/signup", h.Signup)

	w := postForm(t, r, "/signup", url.Values{"email": {"y@example.com"}, "password": {"short"}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("short password: got %d, want 400", w.Code)
	}
}

func TestChangePassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestHandlers(t)

	ctx := context.Background()
	hash, err := auth.HashPassword("password1")
	if err != nil {
		t.Fatal(err)
	}
	uid, err := h.store.CreateUser(ctx, "acc@example.com", hash)
	if err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", uid); c.Set("userEmail", "acc@example.com") })
	r.POST("/account/password", h.ChangePassword)

	// Wrong current password is rejected.
	w := postForm(t, r, "/account/password", url.Values{"current": {"wrong"}, "next": {"newpassword"}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("wrong current: got %d, want 400", w.Code)
	}

	// Correct current password succeeds and actually changes the hash.
	w = postForm(t, r, "/account/password", url.Values{"current": {"password1"}, "next": {"newpassword"}})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Password changed") {
		t.Fatalf("change password: %d %s", w.Code, w.Body.String())
	}
	u, _ := h.store.GetUserByID(ctx, uid)
	if ok, _ := auth.VerifyPassword(u.PasswordHash, "newpassword"); !ok {
		t.Fatal("password hash was not updated to the new password")
	}
}

func TestAvatarUploadAndServe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestHandlers(t)

	ctx := context.Background()
	hash, _ := auth.HashPassword("password1")
	uid, err := h.store.CreateUser(ctx, "acc@example.com", hash)
	if err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", uid); c.Set("userEmail", "acc@example.com") })
	r.POST("/account/avatar", h.UpdateAvatar)
	r.POST("/account/avatar/remove", h.RemoveAvatar)
	r.GET("/account/avatar", h.ServeAvatar)

	// Before any upload, serving 404s.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/account/avatar", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("serve before upload: got %d, want 404", w.Code)
	}

	// Upload a small PNG.
	body, contentType := multipartImage(t, "avatar.png", tinyPNG(t))
	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/account/avatar", body)
	req.Header.Set("Content-Type", contentType)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Avatar updated") {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}

	// It's stored and served as a PNG.
	if data, _ := h.store.GetUserAvatar(ctx, uid); len(data) == 0 {
		t.Fatal("avatar not stored")
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/account/avatar", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("serve: got %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}

	// A non-image upload is rejected.
	body, contentType = multipartImage(t, "notes.txt", []byte("not an image"))
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/account/avatar", body)
	req.Header.Set("Content-Type", contentType)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("non-image upload: got %d, want 400", w.Code)
	}

	// Removing the avatar clears it and reverts to the monogram (version 0).
	w = postForm(t, r, "/account/avatar/remove", url.Values{})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Avatar removed") {
		t.Fatalf("remove: %d %s", w.Code, w.Body.String())
	}
	if data, _ := h.store.GetUserAvatar(ctx, uid); len(data) != 0 {
		t.Error("avatar still stored after remove")
	}
	if u, _ := h.store.GetUserByID(ctx, uid); u.AvatarVersion() != 0 {
		t.Errorf("AvatarVersion after remove = %d, want 0", u.AvatarVersion())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/account/avatar", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("serve after remove: got %d, want 404", w.Code)
	}
}

// tinyPNG returns a 4×4 PNG for upload tests.
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// multipartImage builds a multipart body with one "avatar" file part.
func multipartImage(t *testing.T, filename string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("avatar", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatal(err)
	}
	_ = mw.Close()
	return &buf, mw.FormDataContentType()
}

// captureMailer records the last message so a test can read a token out of the
// emailed link.
type captureMailer struct{ last mail.Message }

func (m *captureMailer) Send(_ context.Context, msg mail.Message) error {
	m.last = msg
	return nil
}

func TestEmailChangeFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st := store.New(openTestDB(t))
	mailer := &captureMailer{}
	svc := auth.NewService(st, mailer, "http://localhost:8080", auth.Config{})
	bill := billing.NewService(st, "", "", "http://localhost:8080", "")
	h := New(st, svc, bill, false, 3600)

	ctx := context.Background()
	hash, _ := auth.HashPassword("password1")
	uid, err := st.CreateUser(ctx, "old@example.com", hash)
	if err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", uid); c.Set("userEmail", "old@example.com") })
	r.POST("/account/email", h.RequestEmailChange)
	r.GET("/account/email/verify", h.ConfirmEmailChange)

	// Wrong password is rejected and nothing is pending.
	w := postForm(t, r, "/account/email", url.Values{"new_email": {"new@example.com"}, "password": {"wrong"}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("wrong password: got %d, want 400", w.Code)
	}
	if p, _ := st.GetPendingEmail(ctx, uid); p != "" {
		t.Errorf("pending email set despite bad password: %q", p)
	}

	// Correct password records the pending change and emails the new address.
	w = postForm(t, r, "/account/email", url.Values{"new_email": {"New@Example.com"}, "password": {"password1"}})
	if w.Code != http.StatusOK {
		t.Fatalf("request: got %d, want 200: %s", w.Code, w.Body.String())
	}
	if p, _ := st.GetPendingEmail(ctx, uid); p != "new@example.com" {
		t.Errorf("pending email = %q, want new@example.com", p)
	}
	if mailer.last.To != "new@example.com" {
		t.Errorf("mail sent to %q, want new@example.com", mailer.last.To)
	}
	// The login email hasn't changed yet.
	if u, _ := st.GetUserByID(ctx, uid); u.Email != "old@example.com" {
		t.Errorf("login email changed before confirmation: %q", u.Email)
	}

	// Extract the token from the emailed link and confirm.
	token := tokenFromLink(t, mailer.last.Text)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/account/email/verify?token="+token, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("confirm: got %d, want 200: %s", w.Code, w.Body.String())
	}
	if u, _ := st.GetUserByID(ctx, uid); u.Email != "new@example.com" {
		t.Errorf("login email after confirm = %q, want new@example.com", u.Email)
	}
	if p, _ := st.GetPendingEmail(ctx, uid); p != "" {
		t.Errorf("pending email not cleared after confirm: %q", p)
	}
}

// tokenFromLink pulls the ?token=... value out of an emailed link.
func tokenFromLink(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, "token=")
	if i < 0 {
		t.Fatalf("no token in mail body: %q", body)
	}
	tok := body[i+len("token="):]
	if j := strings.IndexAny(tok, "\n \t"); j >= 0 {
		tok = tok[:j]
	}
	return tok
}

func TestUpdateProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestHandlers(t)

	ctx := context.Background()
	hash, _ := auth.HashPassword("password1")
	uid, err := h.store.CreateUser(ctx, "acc@example.com", hash)
	if err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", uid); c.Set("userEmail", "acc@example.com") })
	r.POST("/account/profile", h.UpdateProfile)

	// A valid name is saved.
	w := postForm(t, r, "/account/profile", url.Values{"name": {"Ada Lovelace"}})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Profile updated") {
		t.Fatalf("update profile: %d %s", w.Code, w.Body.String())
	}
	if u, _ := h.store.GetUserByID(ctx, uid); u.Name != "Ada Lovelace" {
		t.Errorf("saved name = %q, want %q", u.Name, "Ada Lovelace")
	}

	// An empty name is rejected and the stored name is unchanged.
	w = postForm(t, r, "/account/profile", url.Values{"name": {"   "}})
	if w.Code != http.StatusBadRequest {
		t.Errorf("empty name: got %d, want 400", w.Code)
	}
	if u, _ := h.store.GetUserByID(ctx, uid); u.Name != "Ada Lovelace" {
		t.Errorf("name changed on rejected update = %q", u.Name)
	}
}
