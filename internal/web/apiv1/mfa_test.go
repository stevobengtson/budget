package apiv1

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/descope/virtualwebauthn"
	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp/totp"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/billing"
	"github.com/sbengtson/budget/internal/core/crypto"
	"github.com/sbengtson/budget/internal/core/mail"
	"github.com/sbengtson/budget/internal/core/passkey"
	"github.com/sbengtson/budget/internal/core/store"
)

const apiTestKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// newMFATestAPI builds an API whose auth service can seal TOTP secrets.
func newMFATestAPI(t *testing.T) (*auth.Service, *store.Store, http.Handler) {
	t.Helper()
	sealer, err := crypto.NewSealer(apiTestKey)
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(openTestDB(t))
	svc := auth.NewService(st, mail.NewConsole(), "http://localhost:8080", auth.Config{Sealer: sealer})
	bill := billing.NewService(st, "", "", "", "http://localhost:8080", "")

	gin.SetMode(gin.TestMode)
	r := gin.New()
	New(st, svc, bill).Register(r.Group("/api/v1"))
	return svc, st, r
}

// enrolAPIUser creates a verified user with a confirmed authenticator and
// returns their base32 secret.
func enrolAPIUser(t *testing.T, svc *auth.Service, st *store.Store, email, password string) string {
	t.Helper()
	uid := makeVerifiedUser(t, st, email, password)

	enr, err := svc.BeginTOTPEnrolment(t.Context(), uid)
	if err != nil {
		t.Fatal(err)
	}
	code, err := totp.GenerateCode(enr.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmTOTP(t.Context(), uid, code); err != nil {
		t.Fatal(err)
	}
	return enr.Secret
}

// nextStep returns a code from the step after now. Confirming enrolment
// consumes its own step, so a code from that same window is correctly rejected
// as a replay.
func nextStep(t *testing.T, secret string) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, time.Now().Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	return code
}

// THE compatibility test. Builds already in TestFlight and Play send no "mfa"
// field and decode a flat {token,...}. They must never receive a token for an
// account with a second factor, and must get a message that tells the user what
// to do — both clients render error.message verbatim.
func TestLegacyClientGetsActionableErrorNotAToken(t *testing.T) {
	svc, st, r := newMFATestAPI(t)
	enrolAPIUser(t, svc, st, "legacy@example.com", "password1")

	var body struct {
		Token string `json:"token"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	// No "mfa" field: exactly what a shipped build sends.
	w := doJSON(t, r, http.MethodPost, "/api/v1/login", "",
		`{"email":"legacy@example.com","password":"password1"}`, &body)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if body.Token != "" {
		t.Fatal("a legacy client must never receive a session token for an MFA account")
	}
	if body.Error.Code != "mfa_required" {
		t.Fatalf("error code = %q, want mfa_required", body.Error.Code)
	}
	if body.Error.Message == "" {
		t.Fatal("the message is the only thing the user sees; it must not be empty")
	}
}

// An MFA-aware client gets the challenge and can finish it.
func TestMFAClientCompletesChallenge(t *testing.T) {
	svc, st, r := newMFATestAPI(t)
	secret := enrolAPIUser(t, svc, st, "modern@example.com", "password1")

	var ch struct {
		Status      string   `json:"status"`
		Challenge   string   `json:"challenge"`
		Methods     []string `json:"methods"`
		MaskedEmail string   `json:"maskedEmail"`
		Token       string   `json:"token"`
	}
	w := doJSON(t, r, http.MethodPost, "/api/v1/login", "",
		`{"email":"modern@example.com","password":"password1","mfa":true,"client":"ios"}`, &ch)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ch.Status != "mfa_required" {
		t.Fatalf("status = %q, want mfa_required", ch.Status)
	}
	if ch.Token != "" {
		t.Fatal("a challenge response must carry no session token")
	}
	if ch.Challenge == "" || len(ch.Methods) == 0 {
		t.Fatalf("challenge payload incomplete: %+v", ch)
	}

	var done struct {
		Status string `json:"status"`
		Token  string `json:"token"`
	}
	w2 := doJSON(t, r, http.MethodPost, "/api/v1/login/challenge", "",
		`{"challenge":"`+ch.Challenge+`","method":"totp","code":"`+nextStep(t, secret)+`","client":"ios"}`, &done)
	if w2.Code != http.StatusOK {
		t.Fatalf("challenge status = %d, want 200", w2.Code)
	}
	if done.Status != "ok" || done.Token == "" {
		t.Fatalf("want an issued session, got %+v", done)
	}

	// The issued token must actually work.
	var me struct {
		User struct {
			Email string `json:"email"`
		} `json:"user"`
	}
	w3 := doJSON(t, r, http.MethodGet, "/api/v1/me", done.Token, "", &me)
	if w3.Code != http.StatusOK || me.User.Email != "modern@example.com" {
		t.Fatalf("me = %d %+v", w3.Code, me)
	}
}

// An account with no second factor keeps the flat success shape, so opting in
// costs nothing for users who have not enabled anything.
func TestMFAOptInStillWorksWithoutFactors(t *testing.T) {
	_, st, r := newMFATestAPI(t)
	makeVerifiedUser(t, st, "plain@example.com", "password1")

	var body struct {
		Status string `json:"status"`
		Token  string `json:"token"`
	}
	w := doJSON(t, r, http.MethodPost, "/api/v1/login", "",
		`{"email":"plain@example.com","password":"password1","mfa":true}`, &body)
	if w.Code != http.StatusOK || body.Token == "" {
		t.Fatalf("want a session, got %d %+v", w.Code, body)
	}
	if body.Status != "ok" {
		t.Fatalf("status = %q, want ok", body.Status)
	}
}

func TestChallengeRejectsWrongCode(t *testing.T) {
	svc, st, r := newMFATestAPI(t)
	enrolAPIUser(t, svc, st, "wrong@example.com", "password1")

	var ch struct {
		Challenge string `json:"challenge"`
	}
	doJSON(t, r, http.MethodPost, "/api/v1/login", "",
		`{"email":"wrong@example.com","password":"password1","mfa":true}`, &ch)

	var out struct {
		Token string `json:"token"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	w := doJSON(t, r, http.MethodPost, "/api/v1/login/challenge", "",
		`{"challenge":"`+ch.Challenge+`","method":"totp","code":"000000"}`, &out)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if out.Token != "" {
		t.Fatal("a rejected code must not yield a token")
	}
	if out.Error.Code != "invalid_code" {
		t.Fatalf("error code = %q, want invalid_code", out.Error.Code)
	}
}

// The legacy path must not leave a challenge sitting open — it is a credential
// nobody is holding.
func TestLegacyRejectionAbandonsTheChallenge(t *testing.T) {
	svc, st, r := newMFATestAPI(t)
	enrolAPIUser(t, svc, st, "abandon@example.com", "password1")

	doJSON(t, r, http.MethodPost, "/api/v1/login", "",
		`{"email":"abandon@example.com","password":"password1"}`, nil)

	var n int
	if err := st.DB().QueryRow(
		`SELECT COUNT(*) FROM auth_challenges WHERE consumed_at IS NULL`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("open challenges = %d, want 0", n)
	}
}

func TestSecurityEndpointReportsState(t *testing.T) {
	svc, st, r := newMFATestAPI(t)
	secret := enrolAPIUser(t, svc, st, "state@example.com", "password1")

	var ch struct {
		Challenge string `json:"challenge"`
	}
	doJSON(t, r, http.MethodPost, "/api/v1/login", "",
		`{"email":"state@example.com","password":"password1","mfa":true}`, &ch)
	var done struct {
		Token string `json:"token"`
	}
	doJSON(t, r, http.MethodPost, "/api/v1/login/challenge", "",
		`{"challenge":"`+ch.Challenge+`","method":"totp","code":"`+nextStep(t, secret)+`"}`, &done)

	var sec struct {
		TOTPEnabled       bool `json:"totpEnabled"`
		RecoveryRemaining int  `json:"recoveryRemaining"`
		HasPassword       bool `json:"hasPassword"`
	}
	w := doJSON(t, r, http.MethodGet, "/api/v1/security", done.Token, "", &sec)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !sec.TOTPEnabled || sec.RecoveryRemaining != 10 || !sec.HasPassword {
		t.Fatalf("unexpected security state: %+v", sec)
	}
}

func TestSecurityEndpointRequiresAuth(t *testing.T) {
	_, _, r := newMFATestAPI(t)
	w := doJSON(t, r, http.MethodGet, "/api/v1/security", "", "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

// A full passkey sign-in over the real HTTP surface, driven by a virtual
// authenticator. The unit tests prove the ceremony; this proves the wiring —
// options out, response in, session back.
func TestPasskeyLoginOverTheAPI(t *testing.T) {
	sealer, err := crypto.NewSealer(apiTestKey)
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(openTestDB(t))
	pk, err := passkey.New(st, passkey.Config{
		RPID:          "pigglet.ca",
		RPDisplayName: "Pigglet",
		Origins:       []string{"https://pigglet.ca"},
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := auth.NewService(st, mail.NewConsole(), "http://localhost:8080",
		auth.Config{Sealer: sealer, Passkeys: pk})
	bill := billing.NewService(st, "", "", "", "http://localhost:8080", "")

	gin.SetMode(gin.TestMode)
	r := gin.New()
	New(st, svc, bill).Register(r.Group("/api/v1"))

	uid := makeVerifiedUser(t, st, "passkey-api@example.com", "password1")

	rp := virtualwebauthn.RelyingParty{Name: "Pigglet", ID: "pigglet.ca", Origin: "https://pigglet.ca"}
	authr := virtualwebauthn.NewAuthenticator()
	cred := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)

	// Enrol directly through the service; the registration HTTP path needs a
	// bearer token, which is what we are trying to obtain.
	options, session, err := pk.BeginRegistration(t.Context(), uid)
	if err != nil {
		t.Fatal(err)
	}
	attOpts, err := virtualwebauthn.ParseAttestationOptions(string(options))
	if err != nil {
		t.Fatal(err)
	}
	authr.Options.UserHandle = []byte(attOpts.UserID)
	attestation := virtualwebauthn.CreateAttestationResponse(rp, authr, cred, *attOpts)
	reg, err := pk.FinishRegistration(t.Context(), uid, session, []byte(attestation))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateCredential(t.Context(), store.WebAuthnCredential{
		UserID: uid, CredentialID: reg.CredentialID, PublicKey: reg.PublicKey,
		AAGUID: reg.AAGUID, SignCount: reg.SignCount, Transports: reg.Transports,
		AttestationType: reg.AttestationType, BackupEligible: reg.BackupEligible,
		BackupState: reg.BackupState, Name: "Test key",
	}); err != nil {
		t.Fatal(err)
	}
	authr.AddCredential(cred)

	// Now the actual HTTP sign-in.
	var begin struct {
		Ceremony string          `json:"ceremony"`
		Options  json.RawMessage `json:"options"`
	}
	w := doJSON(t, r, http.MethodPost, "/api/v1/webauthn/login/begin", "", "", &begin)
	if w.Code != http.StatusOK {
		t.Fatalf("begin = %d, want 200", w.Code)
	}
	if begin.Ceremony == "" || len(begin.Options) == 0 {
		t.Fatalf("incomplete begin payload: %+v", begin)
	}

	assertOpts, err := virtualwebauthn.ParseAssertionOptions(string(begin.Options))
	if err != nil {
		t.Fatalf("the options must be usable verbatim by a client: %v", err)
	}
	assertion := virtualwebauthn.CreateAssertionResponse(rp, authr, cred, *assertOpts)

	body, err := json.Marshal(map[string]any{
		"ceremony": begin.Ceremony,
		"response": json.RawMessage(assertion),
		"client":   "ios",
	})
	if err != nil {
		t.Fatal(err)
	}
	var done struct {
		Status string `json:"status"`
		Token  string `json:"token"`
		User   struct {
			Email string `json:"email"`
		} `json:"user"`
	}
	w2 := doJSON(t, r, http.MethodPost, "/api/v1/webauthn/login/finish", "", string(body), &done)
	if w2.Code != http.StatusOK {
		t.Fatalf("finish = %d, want 200", w2.Code)
	}
	// This authenticator verifies the user, so the assertion stands alone as
	// both factors and a session is issued outright.
	if done.Status != "ok" || done.Token == "" {
		t.Fatalf("want an issued session, got %+v", done)
	}
	if done.User.Email != "passkey-api@example.com" {
		t.Fatalf("signed in as %q", done.User.Email)
	}

	// And the token works.
	var me struct {
		User struct {
			Email string `json:"email"`
		} `json:"user"`
	}
	if w3 := doJSON(t, r, http.MethodGet, "/api/v1/me", done.Token, "", &me); w3.Code != http.StatusOK {
		t.Fatalf("me = %d", w3.Code)
	}

	// The credential's last-used timestamp is recorded.
	creds, err := st.ListCredentialsForUser(t.Context(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(creds) != 1 || creds[0].LastUsedAt == nil {
		t.Fatal("a successful assertion should record the credential's use")
	}
}
