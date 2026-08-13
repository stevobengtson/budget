package plaid

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	plaidapi "github.com/plaid/plaid-go/v45/plaid"
)

// keyedFakeAPI serves one webhook verification key.
type keyedFakeAPI struct {
	fakeAPI
	pub *ecdsa.PublicKey
}

func (f *keyedFakeAPI) WebhookVerificationKeyGet(_ context.Context, kid string) (plaidapi.WebhookVerificationKeyGetResponse, error) {
	key := plaidapi.JWKPublicKey{
		Alg: "ES256", Crv: "P-256", Kid: kid, Kty: "EC", Use: "sig",
		X: base64.RawURLEncoding.EncodeToString(f.pub.X.Bytes()),
		Y: base64.RawURLEncoding.EncodeToString(f.pub.Y.Bytes()),
	}
	return plaidapi.WebhookVerificationKeyGetResponse{Key: key}, nil
}

func signWebhook(t *testing.T, priv *ecdsa.PrivateKey, kid string, body []byte, iat time.Time, tamperHash bool) string {
	t.Helper()
	sum := sha256.Sum256(body)
	bodyHash := hex.EncodeToString(sum[:])
	if tamperHash {
		bodyHash = hex.EncodeToString(make([]byte, 32))
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iat":                 iat.Unix(),
		"request_body_sha256": bodyHash,
	})
	tok.Header["kid"] = kid
	signed, err := tok.SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func TestVerifyWebhook(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherPriv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	api := &keyedFakeAPI{pub: &priv.PublicKey}
	s := &Service{api: api, configured: true}
	ctx := context.Background()
	body := []byte(`{"webhook_type":"TRANSACTIONS","webhook_code":"SYNC_UPDATES_AVAILABLE","item_id":"item-1"}`)

	// kid must be unique per case: verified keys are cached process-wide, so a
	// key that once verified would satisfy later cases signed by the same kid.
	if err := s.VerifyWebhook(ctx, body, signWebhook(t, priv, "kid-good", body, time.Now(), false)); err != nil {
		t.Errorf("valid webhook rejected: %v", err)
	}
	if err := s.VerifyWebhook(ctx, body, signWebhook(t, otherPriv, "kid-good", body, time.Now(), false)); !errors.Is(err, ErrWebhookVerification) {
		t.Errorf("wrong-key signature accepted: %v", err)
	}
	if err := s.VerifyWebhook(ctx, body, signWebhook(t, priv, "kid-good", body, time.Now().Add(-10*time.Minute), false)); !errors.Is(err, ErrWebhookVerification) {
		t.Errorf("stale iat accepted: %v", err)
	}
	if err := s.VerifyWebhook(ctx, body, signWebhook(t, priv, "kid-good", body, time.Now(), true)); !errors.Is(err, ErrWebhookVerification) {
		t.Errorf("wrong body hash accepted: %v", err)
	}
	if err := s.VerifyWebhook(ctx, []byte("other body"), signWebhook(t, priv, "kid-good", body, time.Now(), false)); !errors.Is(err, ErrWebhookVerification) {
		t.Errorf("body substitution accepted: %v", err)
	}
	if err := s.VerifyWebhook(ctx, body, ""); !errors.Is(err, ErrWebhookVerification) {
		t.Errorf("missing header accepted: %v", err)
	}

	// Non-ES256 algorithms are rejected even with a valid-looking token.
	hs := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iat":                 time.Now().Unix(),
		"request_body_sha256": func() string { s := sha256.Sum256(body); return hex.EncodeToString(s[:]) }(),
	})
	hs.Header["kid"] = "kid-good"
	hsSigned, _ := hs.SignedString([]byte("secret"))
	if err := s.VerifyWebhook(ctx, body, hsSigned); !errors.Is(err, ErrWebhookVerification) {
		t.Errorf("HS256 token accepted: %v", err)
	}
}
