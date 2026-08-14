package oidcauth

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrAppleKey means the configured .p8 key could not be read.
var ErrAppleKey = errors.New("apple private key is not a valid PKCS#8 EC key")

// newAppleSigner returns a function that mints Apple's client secret.
//
// Apple does not issue a static secret. The "client_secret" it expects is a
// JWT signed with a .p8 EC key, asserting the Team ID as issuer and the
// Services ID as subject. Apple rejects one whose expiry is more than six
// months out.
//
// This mints a fresh, short-lived assertion per exchange rather than caching a
// long one. It costs one signature on a path that is already making a network
// round trip, and it removes the whole class of "the secret quietly expired six
// months after someone set it up" incident — which, being six months later,
// nobody would connect to this code.
func newAppleSigner(cfg AppleConfig) (func() (string, error), error) {
	key, err := parseP8(cfg.PrivateKeyPEM)
	if err != nil {
		return nil, err
	}
	if cfg.TeamID == "" || cfg.KeyID == "" {
		return nil, fmt.Errorf("%w: team id and key id are both required", ErrAppleKey)
	}
	return func() (string, error) {
		now := time.Now()
		tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
			"iss": cfg.TeamID,
			"iat": now.Unix(),
			"exp": now.Add(appleSecretTTL).Unix(),
			"aud": appleIssuer,
			"sub": cfg.ClientID,
		})
		tok.Header["kid"] = cfg.KeyID
		signed, err := tok.SignedString(key)
		if err != nil {
			return "", fmt.Errorf("sign apple client secret: %w", err)
		}
		return signed, nil
	}, nil
}

// parseP8 reads the EC private key out of an Apple .p8 file.
func parseP8(pemText string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, fmt.Errorf("%w: no PEM block", ErrAppleKey)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAppleKey, err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%w: key is %T, not EC", ErrAppleKey, parsed)
	}
	return key, nil
}
