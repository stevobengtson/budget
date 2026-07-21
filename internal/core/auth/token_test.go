package auth

import "testing"

func TestRandomTokenUnique(t *testing.T) {
	a, err := RandomToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := RandomToken()
	if a == b {
		t.Fatal("tokens must be unique")
	}
	if len(a) < 32 {
		t.Fatalf("token too short: %q", a)
	}
}

func TestHashTokenStable(t *testing.T) {
	if HashToken("abc") != HashToken("abc") {
		t.Fatal("hash must be deterministic")
	}
	if HashToken("abc") == "abc" {
		t.Fatal("hash must not equal input")
	}
}
