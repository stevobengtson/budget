package crypto

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

const testKey = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func TestSealOpenRoundtrip(t *testing.T) {
	s, err := NewSealer(testKey)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := s.Seal("access-sandbox-secret-token")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), "access-sandbox") {
		t.Fatal("plaintext visible in sealed blob")
	}
	got, err := s.Open(blob)
	if err != nil {
		t.Fatal(err)
	}
	if got != "access-sandbox-secret-token" {
		t.Errorf("roundtrip = %q", got)
	}

	// Nonces are random: sealing twice must not produce the same blob.
	blob2, _ := s.Seal("access-sandbox-secret-token")
	if string(blob) == string(blob2) {
		t.Error("two seals produced identical blobs (nonce reuse)")
	}
}

func TestOpenRejectsTamperAndWrongKey(t *testing.T) {
	s, _ := NewSealer(testKey)
	blob, _ := s.Seal("secret")

	tampered := append([]byte{}, blob...)
	tampered[len(tampered)-1] ^= 0x01
	if _, err := s.Open(tampered); !errors.Is(err, ErrCorruptBlob) {
		t.Errorf("tampered blob err = %v, want ErrCorruptBlob", err)
	}

	otherKey := hex.EncodeToString(make([]byte, 32))
	other, _ := NewSealer(otherKey)
	if _, err := other.Open(blob); !errors.Is(err, ErrCorruptBlob) {
		t.Errorf("wrong-key open err = %v, want ErrCorruptBlob", err)
	}

	if _, err := s.Open([]byte{9, 9}); !errors.Is(err, ErrCorruptBlob) {
		t.Errorf("short blob err = %v, want ErrCorruptBlob", err)
	}
	// Unknown version byte.
	bad := append([]byte{}, blob...)
	bad[0] = 2
	if _, err := s.Open(bad); !errors.Is(err, ErrCorruptBlob) {
		t.Errorf("bad version err = %v, want ErrCorruptBlob", err)
	}
}

func TestNewSealerRejectsBadKeys(t *testing.T) {
	for _, k := range []string{"", "abcd", "zz" + testKey[2:], testKey + "00"} {
		if _, err := NewSealer(k); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("NewSealer(%q) err = %v, want ErrInvalidKey", k, err)
		}
	}
}
