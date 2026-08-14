package passkey_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/descope/virtualwebauthn"

	"github.com/sbengtson/budget/internal/core/db"
	"github.com/sbengtson/budget/internal/core/passkey"
	"github.com/sbengtson/budget/internal/core/store"
)

const (
	rpID       = "pigglet.ca"
	rpOrigin   = "https://pigglet.ca"
	evilOrigin = "https://evil.example"
)

// testDBLockKey MUST match the other packages' advisory-lock key so all
// DB-backed tests serialize on the shared budget_test database.
const testDBLockKey = 918273645

var testTables = []string{
	"transactions", "budgets", "categories", "category_groups",
	"incomes", "accounts", "verification_tokens", "auth_lockouts", "auth_challenges",
	"recovery_codes", "user_totp", "webauthn_credentials", "oauth_identities", "sessions", "users",
}

func testDSN() string {
	if u := os.Getenv("BUDGET_POSTGRES_URL"); u != "" {
		return u
	}
	return "postgres://postgres:postgres@127.0.0.1:5432/budget_test?sslmode=disable"
}

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
	return conn
}

func newService(t *testing.T, origins ...string) (*passkey.Service, *store.Store, int64) {
	t.Helper()
	if len(origins) == 0 {
		origins = []string{rpOrigin}
	}
	st := store.New(openTestDB(t))
	svc, err := passkey.New(st, passkey.Config{
		RPID:          rpID,
		RPDisplayName: "Pigglet",
		Origins:       origins,
	})
	if err != nil {
		t.Fatal(err)
	}
	uid, err := st.CreateUser(context.Background(), "passkey@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	return svc, st, uid
}

// authenticator builds a virtual authenticator that behaves like a phone: it
// stores discoverable credentials and performs user verification.
//
// The Authenticator is returned by pointer because AddCredential mutates it —
// a copy would accept the credential and then assert with an empty store.
func authenticator() (virtualwebauthn.RelyingParty, *virtualwebauthn.Authenticator, virtualwebauthn.Credential) {
	rp := virtualwebauthn.RelyingParty{Name: "Pigglet", ID: rpID, Origin: rpOrigin}
	auth := virtualwebauthn.NewAuthenticator()
	cred := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)
	return rp, &auth, cred
}

// register runs a full enrolment and stores the credential.
func register(t *testing.T, svc *passkey.Service, st *store.Store, uid int64,
	rp virtualwebauthn.RelyingParty, auth *virtualwebauthn.Authenticator, cred virtualwebauthn.Credential,
) store.WebAuthnCredential {
	t.Helper()
	ctx := context.Background()

	options, session, err := svc.BeginRegistration(ctx, uid)
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	parsed, err := virtualwebauthn.ParseAttestationOptions(string(options))
	if err != nil {
		t.Fatalf("parse options: %v", err)
	}
	// A discoverable credential reports the user handle back at sign-in, which
	// is how the server knows who is signing in without an address being typed.
	// Without this the authenticator asserts with a blank handle and the
	// library rejects it.
	auth.Options.UserHandle = []byte(parsed.UserID)
	response := virtualwebauthn.CreateAttestationResponse(rp, *auth, cred, *parsed)

	reg, err := svc.FinishRegistration(ctx, uid, session, []byte(response))
	if err != nil {
		t.Fatalf("finish registration: %v", err)
	}
	row := store.WebAuthnCredential{
		UserID:          uid,
		CredentialID:    reg.CredentialID,
		PublicKey:       reg.PublicKey,
		AAGUID:          reg.AAGUID,
		SignCount:       reg.SignCount,
		Transports:      reg.Transports,
		AttestationType: reg.AttestationType,
		BackupEligible:  reg.BackupEligible,
		BackupState:     reg.BackupState,
		Name:            "Test key",
	}
	if err := st.CreateCredential(ctx, row); err != nil {
		t.Fatal(err)
	}
	auth.AddCredential(cred)
	return row
}

func TestRegisterThenSignIn(t *testing.T) {
	svc, st, uid := newService(t)
	ctx := context.Background()
	rp, auth, cred := authenticator()
	register(t, svc, st, uid, rp, auth, cred)

	options, session, err := svc.BeginLogin(ctx)
	if err != nil {
		t.Fatalf("begin login: %v", err)
	}
	parsed, err := virtualwebauthn.ParseAssertionOptions(string(options))
	if err != nil {
		t.Fatalf("parse assertion options: %v", err)
	}
	response := virtualwebauthn.CreateAssertionResponse(rp, *auth, cred, *parsed)

	assertion, err := svc.FinishLogin(ctx, session, []byte(response))
	if err != nil {
		t.Fatalf("finish login: %v", err)
	}
	if assertion.UserID != uid {
		t.Fatalf("signed in as %d, want %d", assertion.UserID, uid)
	}
	if !assertion.UserVerified {
		t.Error("this authenticator performs user verification; the flag should say so")
	}
	if assertion.CloneWarning {
		t.Error("a first sign-in must not look like a clone")
	}
}

// THE test that proves the origin allowlist is enforced. Everything else about
// WebAuthn can be right and the whole scheme is still worthless if an assertion
// minted by a phishing site is accepted.
func TestAssertionFromAnotherOriginIsRejected(t *testing.T) {
	svc, st, uid := newService(t)
	ctx := context.Background()
	rp, auth, cred := authenticator()
	register(t, svc, st, uid, rp, auth, cred)

	options, session, err := svc.BeginLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := virtualwebauthn.ParseAssertionOptions(string(options))
	if err != nil {
		t.Fatal(err)
	}
	// Same authenticator, same credential — but the page asking for it is not
	// ours.
	evil := virtualwebauthn.RelyingParty{Name: "Pigglet", ID: rpID, Origin: evilOrigin}
	response := virtualwebauthn.CreateAssertionResponse(evil, *auth, cred, *parsed)

	if _, err := svc.FinishLogin(ctx, session, []byte(response)); err == nil {
		t.Fatal("an assertion from another origin must be rejected")
	}
}

// Registration is origin-checked too, or an attacker could enrol their own
// authenticator against someone's account from a page they control.
func TestRegistrationFromAnotherOriginIsRejected(t *testing.T) {
	svc, _, uid := newService(t)
	ctx := context.Background()
	_, auth, cred := authenticator()

	options, session, err := svc.BeginRegistration(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := virtualwebauthn.ParseAttestationOptions(string(options))
	if err != nil {
		t.Fatal(err)
	}
	evil := virtualwebauthn.RelyingParty{Name: "Pigglet", ID: rpID, Origin: evilOrigin}
	response := virtualwebauthn.CreateAttestationResponse(evil, *auth, cred, *parsed)

	if _, err := svc.FinishRegistration(ctx, uid, session, []byte(response)); err == nil {
		t.Fatal("a registration from another origin must be rejected")
	}
}

// The Android app's origin is an "android:apk-key-hash:..." string, nothing
// like an https URL. It has to survive configuration unchanged, because a
// mangled entry fails only on Android while web and iOS keep working.
func TestAndroidOriginIsAccepted(t *testing.T) {
	const androidOrigin = "android:apk-key-hash:pNoWpRfCzTDGDbn7yvBAOL9ZoBqBFCEQIZC2gRPyEUY"
	svc, st, uid := newService(t, rpOrigin, androidOrigin)
	ctx := context.Background()

	rp, auth, cred := authenticator()
	register(t, svc, st, uid, rp, auth, cred)

	androidRP := virtualwebauthn.RelyingParty{Name: "Pigglet", ID: rpID, Origin: androidOrigin}
	options, session, err := svc.BeginLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := virtualwebauthn.ParseAssertionOptions(string(options))
	if err != nil {
		t.Fatal(err)
	}
	response := virtualwebauthn.CreateAssertionResponse(androidRP, *auth, cred, *parsed)
	if _, err := svc.FinishLogin(ctx, session, []byte(response)); err != nil {
		t.Fatalf("an allowlisted Android origin should be accepted: %v", err)
	}
}

// Android's Credential Manager rejects options that omit rpId or carry a zero
// timeout, and does so with an error that says nothing useful. The web and iOS
// clients tolerate both, so nothing else would catch it.
func TestOptionsJSONCarriesRPIDAndTimeout(t *testing.T) {
	svc, st, uid := newService(t)
	ctx := context.Background()
	rp, auth, cred := authenticator()
	register(t, svc, st, uid, rp, auth, cred)

	for _, tc := range []struct {
		name string
		get  func() ([]byte, []byte, error)
		path func(map[string]any) map[string]any
	}{
		{"registration", func() ([]byte, []byte, error) { return svc.BeginRegistration(ctx, uid) },
			func(m map[string]any) map[string]any {
				pk, _ := m["publicKey"].(map[string]any)
				rpMap, _ := pk["rp"].(map[string]any)
				return rpMap
			}},
		{"login", func() ([]byte, []byte, error) { return svc.BeginLogin(ctx) },
			func(m map[string]any) map[string]any {
				pk, _ := m["publicKey"].(map[string]any)
				return pk
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options, _, err := tc.get()
			if err != nil {
				t.Fatal(err)
			}
			var m map[string]any
			if err := json.Unmarshal(options, &m); err != nil {
				t.Fatal(err)
			}
			pk, ok := m["publicKey"].(map[string]any)
			if !ok {
				t.Fatalf("options have no publicKey member: %s", options)
			}
			if timeout, _ := pk["timeout"].(float64); timeout <= 0 {
				t.Errorf("timeout = %v, want a positive value", pk["timeout"])
			}
			// registration carries rp.id; assertion carries rpId directly.
			container := tc.path(m)
			if container["id"] == nil && pk["rpId"] == nil {
				t.Errorf("no relying-party id in options: %s", options)
			}
		})
	}
}

// A counter that goes backwards is the documented signal that a credential has
// been duplicated. The library reports it; this pins that we surface it.
func TestCloneWarningSurfacesFromDecreasingCounter(t *testing.T) {
	svc, st, uid := newService(t)
	ctx := context.Background()
	rp, auth, cred := authenticator()
	row := register(t, svc, st, uid, rp, auth, cred)

	// Pretend the stored counter is far ahead of what the authenticator will
	// report, which is exactly what a cloned credential looks like.
	if err := st.RecordCredentialUse(ctx, row.CredentialID, 1000, false); err != nil {
		t.Fatal(err)
	}

	options, session, err := svc.BeginLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := virtualwebauthn.ParseAssertionOptions(string(options))
	if err != nil {
		t.Fatal(err)
	}
	response := virtualwebauthn.CreateAssertionResponse(rp, *auth, cred, *parsed)

	assertion, err := svc.FinishLogin(ctx, session, []byte(response))
	if err != nil {
		t.Fatalf("the assertion itself is valid: %v", err)
	}
	if !assertion.CloneWarning {
		t.Error("a counter going backwards should raise the clone warning")
	}
}

// A credential the server has never seen must not authenticate anyone.
func TestUnknownCredentialIsRejected(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	rp, auth, cred := authenticator()
	auth.AddCredential(cred) // never registered with us

	options, session, err := svc.BeginLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := virtualwebauthn.ParseAssertionOptions(string(options))
	if err != nil {
		t.Fatal(err)
	}
	response := virtualwebauthn.CreateAssertionResponse(rp, *auth, cred, *parsed)

	if _, err := svc.FinishLogin(ctx, session, []byte(response)); err == nil {
		t.Fatal("an unregistered credential must not sign anyone in")
	}
}

// Passkeys are refused rather than misconfigured: an empty RP ID would bind
// every credential to the wrong domain, permanently.
func TestNewRefusesIncompleteConfig(t *testing.T) {
	st := store.New(openTestDB(t))
	for _, cfg := range []passkey.Config{
		{RPID: "", Origins: []string{rpOrigin}},
		{RPID: rpID, Origins: nil},
	} {
		if _, err := passkey.New(st, cfg); err != passkey.ErrUnavailable {
			t.Errorf("New(%+v) = %v, want ErrUnavailable", cfg, err)
		}
	}
}

// A second enrolment must exclude the credential already registered, so the
// authenticator refuses to make a duplicate instead of silently doing so.
func TestRegistrationExcludesExistingCredentials(t *testing.T) {
	svc, st, uid := newService(t)
	ctx := context.Background()
	rp, auth, cred := authenticator()
	row := register(t, svc, st, uid, rp, auth, cred)

	options, _, err := svc.BeginRegistration(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(options), "excludeCredentials") {
		t.Fatalf("second enrolment should exclude what is registered: %s", options)
	}
	var m map[string]any
	if err := json.Unmarshal(options, &m); err != nil {
		t.Fatal(err)
	}
	pk := m["publicKey"].(map[string]any)
	excluded, _ := pk["excludeCredentials"].([]any)
	if len(excluded) != 1 {
		t.Fatalf("excluded %d credentials, want 1", len(excluded))
	}
	_ = row
}
