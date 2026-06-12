// Package middleware provides HTTP middleware for the JSON API. The auth
// middleware verifies BetterAuth-issued JWTs against a cached JWKS so the Go
// service never needs to share a session store or call back to BetterAuth.
package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// contextUserIDKey is the Gin context key under which the authenticated user
// id (the JWT `sub` claim) is stored.
const contextUserIDKey = "userID"

// signingAlg pins the accepted JWT signature algorithm. BetterAuth's jwt()
// plugin defaults to EdDSA (Ed25519); pinning it rejects "none" and algorithm
// confusion attacks.
const signingAlg = "EdDSA"

// Verifier validates JWTs against a JWK Set.
type Verifier struct {
	kf       keyfunc.Keyfunc
	issuer   string
	audience string
}

// NewVerifier builds a Verifier that fetches and caches the JWK Set from
// jwksURL. The cached client refreshes in the background and re-fetches when a
// token presents an unknown key id, so key rotation needs no restart.
//
// Note: the JWKS endpoint (the BetterAuth/web service) must be reachable at
// startup — the initial fetch happens here.
func NewVerifier(ctx context.Context, jwksURL, issuer, audience string) (*Verifier, error) {
	kf, err := keyfunc.NewDefaultCtx(ctx, []string{jwksURL})
	if err != nil {
		return nil, fmt.Errorf("init jwks from %q: %w", jwksURL, err)
	}
	return newVerifier(kf, issuer, audience), nil
}

// newVerifier wires a Verifier around an existing Keyfunc. Used by NewVerifier
// and by tests (which inject a Keyfunc built from a static JWK Set).
func newVerifier(kf keyfunc.Keyfunc, issuer, audience string) *Verifier {
	return &Verifier{kf: kf, issuer: issuer, audience: audience}
}

// Middleware authenticates each request: it requires a valid Bearer JWT and,
// on success, stores the `sub` claim as the user id in the Gin context. Any
// failure aborts with 401 and no further handlers run.
func (v *Verifier) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, err := bearerToken(c.Request)
		if err != nil {
			abortUnauthorized(c)
			return
		}

		token, err := jwt.Parse(
			raw,
			v.kf.Keyfunc,
			jwt.WithValidMethods([]string{signingAlg}),
			jwt.WithIssuer(v.issuer),
			jwt.WithAudience(v.audience),
			jwt.WithExpirationRequired(),
		)
		if err != nil || !token.Valid {
			abortUnauthorized(c)
			return
		}

		sub, err := token.Claims.GetSubject()
		if err != nil || sub == "" {
			abortUnauthorized(c)
			return
		}

		c.Set(contextUserIDKey, sub)
		c.Next()
	}
}

// UserID returns the authenticated user id placed in the context by
// Middleware. It is empty if the request was not authenticated.
func UserID(c *gin.Context) string {
	return c.GetString(contextUserIDKey)
}

// bearerToken extracts the token from an "Authorization: Bearer <token>"
// header.
func bearerToken(r *http.Request) (string, error) {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", errors.New("missing Authorization header")
	}
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", errors.New("malformed Authorization header")
	}
	return strings.TrimSpace(h[len(prefix):]), nil
}

// abortUnauthorized ends the request with a generic 401. Details are
// intentionally not leaked to the client.
func abortUnauthorized(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
}
