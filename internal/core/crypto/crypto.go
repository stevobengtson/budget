// Package crypto provides authenticated encryption for secrets the app must
// store reversibly (e.g. Plaid access tokens). AES-256-GCM with a random nonce
// per sealing; the key comes from configuration and never lives in the
// database. Blobs are versioned so the key or scheme can rotate later without
// re-encrypting everything up front.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
)

// blobVersion tags the sealed format: 1 = AES-256-GCM, 12-byte nonce.
const blobVersion = 1

const nonceSize = 12

// ErrInvalidKey means the configured key is not 64 hex characters (32 bytes).
var ErrInvalidKey = errors.New("encryption key must be 64 hex characters (32 bytes)")

// ErrCorruptBlob means a stored blob failed authentication or is malformed —
// wrong key, truncated data, or tampering.
var ErrCorruptBlob = errors.New("sealed blob is corrupt or key is wrong")

// Sealer encrypts and decrypts short secrets with a fixed key.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer builds a Sealer from a 64-hex-character key.
func NewSealer(hexKey string) (*Sealer, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil || len(key) != 32 {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	return &Sealer{aead: aead}, nil
}

// Seal encrypts plaintext into a self-describing blob:
// version(1) || nonce(12) || ciphertext.
func (s *Sealer) Seal(plaintext string) ([]byte, error) {
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	out := make([]byte, 0, 1+nonceSize+len(plaintext)+s.aead.Overhead())
	out = append(out, blobVersion)
	out = append(out, nonce...)
	return s.aead.Seal(out, nonce, []byte(plaintext), nil), nil
}

// Open decrypts a blob produced by Seal.
func (s *Sealer) Open(blob []byte) (string, error) {
	if len(blob) < 1+nonceSize || blob[0] != blobVersion {
		return "", ErrCorruptBlob
	}
	plaintext, err := s.aead.Open(nil, blob[1:1+nonceSize], blob[1+nonceSize:], nil)
	if err != nil {
		return "", ErrCorruptBlob
	}
	return string(plaintext), nil
}
