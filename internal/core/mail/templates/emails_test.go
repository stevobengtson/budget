package emailtpl

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sbengtson/budget/internal/core/i18n"
)

// untranslatedID matches a message id that reached the output unresolved, e.g.
// "email.verify_intro". It requires the underscore so that ordinary prose ending
// in "...ignore this email." is not mistaken for one.
var untranslatedID = regexp.MustCompile(`email\.[a-z]+_[a-z_]+`)

// Every message must render in every language we ship, with no untranslated
// message IDs leaking through and every part populated. A missing catalog key
// renders as the raw ID (e.g. "email.verify_intro"), which is exactly the
// failure this catches.
func TestEveryMessageRendersInEveryLocale(t *testing.T) {
	const link = "https://pigglet.ca/verify?token=abc123"

	for _, l := range i18n.Supported {
		for _, tc := range []struct {
			name string
			run  func() (subject, html, text string, err error)
		}{
			{"verify", func() (string, string, string, error) {
				m, err := VerifyEmail(l, link, time.Hour)
				return m.Subject, m.HTML, m.Text, err
			}},
			{"reset", func() (string, string, string, error) {
				m, err := PasswordReset(l, link, time.Hour)
				return m.Subject, m.HTML, m.Text, err
			}},
			{"email_change", func() (string, string, string, error) {
				m, err := EmailChange(l, link, time.Hour)
				return m.Subject, m.HTML, m.Text, err
			}},
			{"lockout", func() (string, string, string, error) {
				m, err := LockoutAlert(l, "https://pigglet.ca/forgot")
				return m.Subject, m.HTML, m.Text, err
			}},
			{"password_changed", func() (string, string, string, error) {
				m, err := PasswordChanged(l)
				return m.Subject, m.HTML, m.Text, err
			}},
			{"security_changed", func() (string, string, string, error) {
				m, err := SecurityChanged(l, SecurityEvent{Kind: SecurityEventTOTPDisabled})
				return m.Subject, m.HTML, m.Text, err
			}},
		} {
			t.Run(string(l)+"/"+tc.name, func(t *testing.T) {
				subject, html, text, err := tc.run()
				if err != nil {
					t.Fatal(err)
				}
				if subject == "" || html == "" || text == "" {
					t.Fatalf("empty part: subject=%q html=%d bytes text=%d bytes",
						subject, len(html), len(text))
				}
				for _, part := range []string{subject, html, text} {
					if id := untranslatedID.FindString(part); id != "" {
						t.Fatalf("untranslated message id %q leaked into output", id)
					}
				}
			})
		}
	}
}

// The two catalogs must actually differ, or a copy-paste in the TOML would go
// unnoticed and French speakers would silently receive English.
func TestLocalesProduceDifferentText(t *testing.T) {
	en, err := VerifyEmail(i18n.EnCA, "https://x", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	fr, err := VerifyEmail(i18n.FrCA, "https://x", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if en.Subject == fr.Subject {
		t.Fatalf("en and fr subjects are identical: %q", en.Subject)
	}
}

func TestLinkAppearsInBothHalves(t *testing.T) {
	const link = "https://pigglet.ca/reset?token=xyz"
	m, err := PasswordReset(i18n.EnCA, link, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.HTML, link) {
		t.Fatal("link missing from HTML")
	}
	// The plaintext alternative is what a text-only client shows; a link that
	// only exists in the HTML half strands those readers.
	if !strings.Contains(m.Text, link) {
		t.Fatal("link missing from plaintext")
	}
}

func TestExpiryUsesHoursWhenWhole(t *testing.T) {
	whole, err := VerifyEmail(i18n.EnCA, "https://x", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(whole.Text, "1 hour") {
		t.Fatalf("want a whole-hour phrasing, got: %q", whole.Text)
	}

	partial, err := VerifyEmail(i18n.EnCA, "https://x", 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(partial.Text, "10 minutes") {
		t.Fatalf("want minutes phrasing, got: %q", partial.Text)
	}
}

// A one-time-code mail must never carry a clickable sign-in link, and an alert
// about something that already happened must not either — an alert that can
// itself authenticate turns every security notice into a phishing lure.
func TestPasswordChangedAlertCarriesNoActionLink(t *testing.T) {
	m, err := PasswordChanged(i18n.EnCA)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(m.HTML, "token=") {
		t.Fatal("an alert email must not contain a credential-bearing link")
	}
}

// A code email contains no link, so telling the reader "this link expires"
// sends them looking for one that is not there.
func TestCodeEmailDoesNotMentionALink(t *testing.T) {
	for _, l := range i18n.Supported {
		m, err := LoginCode(l, "123456", 10*time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		for _, part := range []string{m.HTML, m.Text} {
			lower := strings.ToLower(part)
			if strings.Contains(lower, "this link expires") || strings.Contains(lower, "ce lien expire") {
				t.Errorf("%s: code email must not talk about a link: %q", l, part)
			}
		}
		if !strings.Contains(m.Text, "123456") {
			t.Errorf("%s: the code itself is missing from the plaintext", l)
		}
		// And it must not carry anything clickable that could sign someone in.
		if strings.Contains(m.HTML, "<a ") {
			t.Errorf("%s: a code email must contain no links at all", l)
		}
	}
}

// The notification has to name WHAT changed. "Your settings changed" is close
// to useless in the case that matters: someone judging whether a change was
// theirs needs to know which one it was.
func TestSecurityChangedNamesTheChange(t *testing.T) {
	for _, tc := range []struct{ kind, want string }{
		{SecurityEventTOTPEnabled, "turned ON"},
		{SecurityEventTOTPDisabled, "turned OFF"},
		{SecurityEventEmailOTPEnabled, "turned ON"},
		{SecurityEventEmailOTPDisabled, "turned OFF"},
		{SecurityEventRecoveryRegenerated, "New recovery codes"},
	} {
		m, err := SecurityChanged(i18n.EnCA, SecurityEvent{Kind: tc.kind})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(m.Text, tc.want) {
			t.Errorf("%s: want %q in the body, got %q", tc.kind, tc.want, m.Text)
		}
		// Without CodesCleared it must not claim codes were deleted.
		if strings.Contains(m.Text, "recovery codes were deleted") {
			t.Errorf("%s: must not claim codes were deleted", tc.kind)
		}
	}
}

// Losing recovery codes is the change a user is least likely to notice on their
// own — a saved printout gives no sign it has stopped working — so the email
// has to say it outright.
func TestSecurityChangedSpellsOutClearedCodes(t *testing.T) {
	for _, l := range i18n.Supported {
		m, err := SecurityChanged(l, SecurityEvent{Kind: SecurityEventTOTPDisabled, CodesCleared: true})
		if err != nil {
			t.Fatal(err)
		}
		if id := untranslatedID.FindString(m.HTML); id != "" {
			t.Fatalf("%s: untranslated id %q", l, id)
		}
		if len(m.Text) == 0 {
			t.Fatalf("%s: empty body", l)
		}
	}

	en, err := SecurityChanged(i18n.EnCA, SecurityEvent{Kind: SecurityEventTOTPDisabled, CodesCleared: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(en.Text, "recovery codes were deleted") {
		t.Errorf("the email must say the codes are gone, got: %q", en.Text)
	}
	if !strings.Contains(en.Text, "no longer work") {
		t.Errorf("it must say a saved copy is now useless, got: %q", en.Text)
	}
}

// An unrecognised kind must still produce a sendable email rather than leaking
// a raw message id to the reader.
func TestSecurityChangedFallsBackForUnknownKind(t *testing.T) {
	m, err := SecurityChanged(i18n.EnCA, SecurityEvent{})
	if err != nil {
		t.Fatal(err)
	}
	if id := untranslatedID.FindString(m.HTML); id != "" {
		t.Fatalf("untranslated id leaked: %q", id)
	}
}
