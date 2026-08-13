package plaid

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/sbengtson/budget/internal/core/store"
)

// ErrWebhookVerification means the Plaid-Verification JWT failed any check:
// signature, algorithm, age, or body hash. Handlers respond 401.
var ErrWebhookVerification = errors.New("plaid webhook verification failed")

// webhookMaxAge rejects replayed webhooks per Plaid's guidance.
const webhookMaxAge = 5 * time.Minute

// keyCache caches webhook verification JWKs by kid; Plaid rotates keys rarely
// and recommends caching to avoid an API round-trip per webhook.
var keyCache sync.Map // kid -> *ecdsa.PublicKey

// webhookPayload is the slice of Plaid's webhook body this app acts on. The
// payload is only trusted AFTER VerifyWebhook has authenticated the request.
type webhookPayload struct {
	WebhookType string `json:"webhook_type"`
	WebhookCode string `json:"webhook_code"`
	ItemID      string `json:"item_id"`
}

// HandleWebhook verifies and processes one webhook request. Unhandled codes
// are a no-op success; unknown items likewise (e.g. events trailing an
// /item/remove), so Plaid does not endlessly retry them.
//
// entitled gates the actual sync work per owning user — an item whose owner
// no longer has (or pays for) the add-on stays quiet without erroring the
// webhook.
func (s *Service) HandleWebhook(ctx context.Context, body []byte, jwtHeader string, entitled func(context.Context, int64) bool) error {
	if !s.configured {
		return ErrNotConfigured
	}
	if err := s.VerifyWebhook(ctx, body, jwtHeader); err != nil {
		return err
	}
	var p webhookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return fmt.Errorf("parse webhook body: %w", err)
	}
	if p.ItemID == "" {
		return nil
	}
	item, err := s.store.GetPlaidItemByItemID(ctx, p.ItemID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}

	switch p.WebhookType {
	case "TRANSACTIONS":
		if p.WebhookCode != "SYNC_UPDATES_AVAILABLE" {
			return nil
		}
		if entitled != nil && !entitled(ctx, item.UserID) {
			return nil
		}
		// Sync in the background so the webhook responds fast; the per-item
		// advisory lock dedupes bursts. The request context dies with the
		// response, so the sync gets its own.
		go func() {
			syncCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			_ = s.SyncItem(syncCtx, item)
		}()
	case "ITEM":
		switch p.WebhookCode {
		case "ERROR", "LOGIN_REPAIRED":
			// ERROR almost always means ITEM_LOGIN_REQUIRED; LOGIN_REPAIRED means
			// Plaid fixed it without the user. A sync confirms either way: it
			// resets status to active on success or records login_required.
			if entitled == nil || entitled(ctx, item.UserID) {
				go func() {
					syncCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
					defer cancel()
					_ = s.SyncItem(syncCtx, item)
				}()
			}
		case "PENDING_EXPIRATION", "PENDING_DISCONNECT":
			return s.store.SetPlaidItemStatus(ctx, item.ID, store.PlaidItemPendingExpiration, p.WebhookCode)
		case "USER_PERMISSION_REVOKED", "USER_ACCOUNT_REVOKED":
			return s.store.SetPlaidItemStatus(ctx, item.ID, store.PlaidItemLoginRequired, p.WebhookCode)
		}
	}
	return nil
}

// VerifyWebhook authenticates a webhook request per Plaid's scheme: the
// Plaid-Verification header carries an ES256 JWT whose claims commit to the
// request body's SHA-256.
func (s *Service) VerifyWebhook(ctx context.Context, body []byte, jwtHeader string) error {
	if jwtHeader == "" {
		return ErrWebhookVerification
	}
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(jwtHeader, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("missing kid")
		}
		return s.verificationKey(ctx, kid)
	}, jwt.WithValidMethods([]string{"ES256"}))
	if err != nil || !token.Valid {
		return ErrWebhookVerification
	}

	// Freshness: reject webhooks older than five minutes (replay protection).
	iat, err := claims.GetIssuedAt()
	if err != nil || iat == nil || time.Since(iat.Time) > webhookMaxAge {
		return ErrWebhookVerification
	}

	// Body integrity: the JWT commits to the body's SHA-256.
	claimed, _ := claims["request_body_sha256"].(string)
	sum := sha256.Sum256(body)
	actual := hex.EncodeToString(sum[:])
	if claimed == "" || subtle.ConstantTimeCompare([]byte(claimed), []byte(actual)) != 1 {
		return ErrWebhookVerification
	}
	return nil
}

// verificationKey resolves a webhook verification JWK by key id, caching
// positives. Cache misses hit /webhook_verification_key/get, which also
// authenticates that the kid is really Plaid's.
func (s *Service) verificationKey(ctx context.Context, kid string) (*ecdsa.PublicKey, error) {
	if k, ok := keyCache.Load(kid); ok {
		return k.(*ecdsa.PublicKey), nil
	}
	resp, err := s.api.WebhookVerificationKeyGet(ctx, kid)
	if err != nil {
		return nil, fmt.Errorf("webhook key get: %s", errorCode(err))
	}
	key, err := jwkToECDSA(resp.Key.GetCrv(), resp.Key.GetX(), resp.Key.GetY())
	if err != nil {
		return nil, err
	}
	keyCache.Store(kid, key)
	return key, nil
}

// jwkToECDSA builds a P-256 public key from a JWK's curve name and base64url
// coordinates.
func jwkToECDSA(crv, xs, ys string) (*ecdsa.PublicKey, error) {
	if crv != "P-256" {
		return nil, fmt.Errorf("unsupported curve %q", crv)
	}
	x, err := base64.RawURLEncoding.DecodeString(xs)
	if err != nil {
		return nil, err
	}
	y, err := base64.RawURLEncoding.DecodeString(ys)
	if err != nil {
		return nil, err
	}
	return &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     new(big.Int).SetBytes(x),
		Y:     new(big.Int).SetBytes(y),
	}, nil
}
