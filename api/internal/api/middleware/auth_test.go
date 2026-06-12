package middleware

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const (
	testIssuer   = "http://localhost:3005"
	testAudience = "budget-go-api"
	testKID      = "test-key-1"
	testSubject  = "11111111-1111-1111-1111-111111111111"
)

// newTestVerifier builds a Verifier backed by a static JWK Set holding the
// public half of the returned signing key — mirroring BetterAuth's EdDSA JWKS
// without needing a live HTTP endpoint.
func newTestVerifier(t *testing.T) (*Verifier, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	x := base64.RawURLEncoding.EncodeToString(pub)
	jwksJSON := fmt.Sprintf(
		`{"keys":[{"kty":"OKP","crv":"Ed25519","alg":"EdDSA","kid":%q,"x":%q}]}`,
		testKID, x,
	)
	kf, err := keyfunc.NewJWKSetJSON(json.RawMessage(jwksJSON))
	if err != nil {
		t.Fatalf("build keyfunc: %v", err)
	}
	return newVerifier(kf, testIssuer, testAudience), priv
}

func validClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"sub": testSubject,
		"iss": testIssuer,
		"aud": testAudience,
		"exp": time.Now().Add(time.Hour).Unix(),
	}
}

func signEdDSA(t *testing.T, priv ed25519.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	tok.Header["kid"] = testKID
	s, err := tok.SignedString(priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

// runRequest sends a request through a router guarded by the verifier and
// returns the recorded response. An empty authHeader omits the header.
func runRequest(v *Verifier, authHeader string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(v.Middleware())
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, UserID(c)) })

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestMiddleware_AcceptsValidToken(t *testing.T) {
	v, priv := newTestVerifier(t)
	token := signEdDSA(t, priv, validClaims())

	w := runRequest(v, "Bearer "+token)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%q)", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != testSubject {
		t.Fatalf("userID = %q, want %q", got, testSubject)
	}
}

func TestMiddleware_Rejects(t *testing.T) {
	v, priv := newTestVerifier(t)

	expired := validClaims()
	expired["exp"] = time.Now().Add(-time.Hour).Unix()

	wrongAud := validClaims()
	wrongAud["aud"] = "some-other-api"

	wrongIss := validClaims()
	wrongIss["iss"] = "http://evil.example.com"

	noSub := validClaims()
	delete(noSub, "sub")

	// A token signed with HS256 — pinned-alg check must reject it pre-verify.
	hsClaims := jwt.MapClaims(validClaims())
	hsTok := jwt.NewWithClaims(jwt.SigningMethodHS256, hsClaims)
	hsTok.Header["kid"] = testKID
	hsSigned, err := hsTok.SignedString([]byte("a-shared-secret"))
	if err != nil {
		t.Fatalf("sign hs256: %v", err)
	}

	cases := []struct {
		name   string
		header string
	}{
		{"no header", ""},
		{"not bearer", "Token abc"},
		{"garbage token", "Bearer not-a-jwt"},
		{"expired", "Bearer " + signEdDSA(t, priv, expired)},
		{"wrong audience", "Bearer " + signEdDSA(t, priv, wrongAud)},
		{"wrong issuer", "Bearer " + signEdDSA(t, priv, wrongIss)},
		{"missing subject", "Bearer " + signEdDSA(t, priv, noSub)},
		{"wrong algorithm (HS256)", "Bearer " + hsSigned},
		{"tampered signature", "Bearer " + signEdDSA(t, priv, validClaims()) + "x"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := runRequest(v, tc.header)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body=%q)", w.Code, w.Body.String())
			}
		})
	}
}
