// Package auth provides password hashing, opaque tokens, and the auth service
// that orchestrates the store and mailer for signup/login/verify/reset flows.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters. memory in KiB. Tune upward as hardware allows.
const (
	argonTime    = 1
	argonMemory  = 64 * 1024 // 64 MiB
	argonThreads = 4
	argonKeyLen  = 32
	argonSaltLen = 16
)

const (
	// MinPasswordLength is the shortest password accepted anywhere.
	MinPasswordLength = 8
	// MaxPasswordLength caps what will be fed to argon2id. This is a denial-of-
	// service control, not pedantry: hashing is deliberately expensive, so an
	// unbounded input lets one request burn CPU proportional to its own size.
	MaxPasswordLength = 256
)

var (
	ErrPasswordTooShort = errors.New("password too short")
	ErrPasswordTooLong  = errors.New("password too long")
)

// ValidatePassword enforces the password policy. It lives here rather than in
// the HTTP handlers because passwords are also set through paths that never
// touch a handler — the JSON API, and (in later phases) magic-link signup and
// "add a password to an OAuth account".
func ValidatePassword(password string) error {
	if len(password) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	if len(password) > MaxPasswordLength {
		return ErrPasswordTooLong
	}
	return nil
}

// HashPassword returns a PHC-formatted argon2id hash string.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("gen salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether password matches the encoded argon2id hash.
//
// An empty encoded hash is a plain "no", not an error: since migration 00021 a
// user may legitimately have no password at all (passkey-only, OAuth-only), and
// every caller would otherwise have to distinguish "wrong password" from "this
// account does not use passwords" only to treat them identically.
func VerifyPassword(encoded, password string) (bool, error) {
	if encoded == "" {
		return false, nil
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("bad hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, err
	}
	var mem, t, p uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &t, &p); err != nil {
		return false, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, t, mem, uint8(p), uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// dummyHash is a real argon2id hash, computed once on first use. Nothing can
// match it: the plaintext is random bytes generated at runtime.
var dummyHash = sync.OnceValue(func() string {
	filler := make([]byte, 32)
	if _, err := rand.Read(filler); err != nil {
		// Falling back to a fixed string only costs unpredictability of a value
		// that is never compared against anything real.
		return mustHash("password-that-is-never-used")
	}
	return mustHash(string(filler))
})

func mustHash(s string) string {
	h, err := HashPassword(s)
	if err != nil {
		panic("auth: cannot hash: " + err.Error())
	}
	return h
}

// BurnPasswordTime spends the same work verifying a throwaway hash that a real
// verification would cost.
//
// Login returns early when no user matches the address, which would otherwise
// make "is this email registered" measurable from the outside: a hit pays for
// argon2id, a miss does not. Calling this on the miss path flattens that.
func BurnPasswordTime(password string) {
	_, _ = VerifyPassword(dummyHash(), password)
}
