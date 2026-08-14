package emailtpl

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/sbengtson/budget/internal/core/i18n"
	"github.com/sbengtson/budget/internal/core/mail"
)

// linkData / codeData / alertData are the rendered strings for one message in
// one language. Everything is pre-translated before it reaches a template, so
// the templates hold layout and nothing else.
type linkData struct {
	Heading       string
	Intro         string
	CTA           string
	Link          string
	FallbackIntro string
	Expiry        string
	Ignore        string
}

type codeData struct {
	Heading string
	Intro   string
	Code    string
	Expiry  string
	Ignore  string
}

type alertData struct {
	Heading string
	Intro   string
	Detail  string
	Advice  string
}

// VerifyEmail is the address-confirmation mail sent at registration.
func VerifyEmail(l i18n.Locale, link string, ttl time.Duration) (mail.Message, error) {
	return linkMessage(l, "verify", link, ttl)
}

// PasswordReset carries the "set a new password" link.
func PasswordReset(l i18n.Locale, link string, ttl time.Duration) (mail.Message, error) {
	return linkMessage(l, "reset", link, ttl)
}

// EmailChange confirms a new address, and is sent to that new address.
func EmailChange(l i18n.Locale, link string, ttl time.Duration) (mail.Message, error) {
	return linkMessage(l, "email_change", link, ttl)
}

// LoginCode carries a one-time sign-in code. No link at all: the recipient
// types the code into a device that is already waiting for it, which may not be
// the device reading the mail — and a mail that could itself complete a sign-in
// would make every one of these a phishing target.
func LoginCode(l i18n.Locale, code string, ttl time.Duration) (mail.Message, error) {
	ctx := ctxFor(l)
	d := codeData{
		Heading: i18n.T(ctx, "email.login_code_heading"),
		Intro:   i18n.T(ctx, "email.login_code_intro"),
		Code:    code,
		Expiry:  codeExpiryLine(ctx, ttl),
		Ignore:  i18n.T(ctx, "email.login_code_ignore"),
	}
	text := joinText(d.Intro, code, d.Expiry+" "+d.Ignore)
	return render(ctx, codeEmail(d), i18n.T(ctx, "email.login_code_subject"), text)
}

// MagicLink carries a sign-in link AND the same one-time code.
//
// Both, because the device reading the mail is not always the device signing
// in: clicking works on a phone, and the code covers the case where the link
// opens somewhere useless. They are two presentations of one challenge, not two
// credentials.
func MagicLink(l i18n.Locale, link, code string, ttl time.Duration) (mail.Message, error) {
	ctx := ctxFor(l)
	d := linkData{
		Heading:       i18n.T(ctx, "email.magic_heading"),
		Intro:         i18n.T(ctx, "email.magic_intro"),
		CTA:           i18n.T(ctx, "email.magic_cta"),
		Link:          link,
		FallbackIntro: i18n.Tf(ctx, "email.magic_code_fallback", i18n.M{"Code": code}),
		Expiry:        expiryLine(ctx, ttl),
		Ignore:        i18n.T(ctx, "email.magic_ignore"),
	}
	text := joinText(d.Intro, link, i18n.Tf(ctx, "email.magic_code_fallback", i18n.M{"Code": code}),
		d.Expiry+" "+d.Ignore)
	return render(ctx, linkEmail(d), i18n.T(ctx, "email.magic_subject"), text)
}

// LockoutAlert tells someone their account is being hammered. It is sent once
// per lock, when the lock is applied — never on the blocked attempts that
// follow, or the lockout would become a way to flood the victim's inbox.
func LockoutAlert(l i18n.Locale, resetLink string) (mail.Message, error) {
	ctx := ctxFor(l)
	d := alertData{
		Heading: i18n.T(ctx, "email.lockout_heading"),
		Intro:   i18n.T(ctx, "email.lockout_intro"),
		Detail:  i18n.T(ctx, "email.lockout_detail"),
		Advice:  i18n.Tf(ctx, "email.lockout_advice", i18n.M{"Link": resetLink}),
	}
	text := joinText(d.Intro, d.Detail, d.Advice)
	return render(ctx, alertEmail(d), i18n.T(ctx, "email.lockout_subject"), text)
}

// SecurityEvent describes a change to sign-in settings precisely enough for the
// notification to name it.
//
// A generic "your settings changed" is close to useless in the case that
// matters: someone whose account was taken over needs to know WHAT changed to
// judge whether it was them, and losing recovery codes is the change they are
// least likely to notice on their own.
type SecurityEvent struct {
	// Kind selects the catalog line describing the change. Must be one of the
	// SecurityEvent* constants.
	Kind string
	// CodesCleared reports that the change also invalidated the account's
	// recovery codes.
	CodesCleared bool
}

// Recognised SecurityEvent kinds. The value is the catalog key suffix.
const (
	SecurityEventTOTPEnabled         = "totp_enabled"
	SecurityEventTOTPDisabled        = "totp_disabled"
	SecurityEventEmailOTPEnabled     = "email_otp_enabled"
	SecurityEventEmailOTPDisabled    = "email_otp_disabled"
	SecurityEventRecoveryRegenerated = "recovery_regenerated"
	SecurityEventPasskeyAdded        = "passkey_added"
	SecurityEventPasskeyRemoved      = "passkey_removed"
	SecurityEventIdentityLinked      = "identity_linked"
	SecurityEventIdentityUnlinked    = "identity_unlinked"
)

// SecurityChanged reports a change to sign-in settings, so a change the account
// holder did not make is visible to them.
func SecurityChanged(l i18n.Locale, ev SecurityEvent) (mail.Message, error) {
	ctx := ctxFor(l)
	kind := ev.Kind
	if kind == "" {
		kind = SecurityEventRecoveryRegenerated
	}
	d := alertData{
		Heading: i18n.T(ctx, "email.security_changed_heading"),
		Intro:   i18n.T(ctx, "email.change_"+kind),
		Advice:  i18n.T(ctx, "email.security_changed_advice"),
	}
	// Spelled out rather than left for the user to discover: a printout of
	// recovery codes gives no sign that it has stopped working.
	if ev.CodesCleared {
		d.Detail = i18n.T(ctx, "email.security_codes_cleared")
	}
	return render(ctx, alertEmail(d), i18n.T(ctx, "email.security_changed_subject"),
		joinText(d.Intro, d.Detail, d.Advice))
}

// PasswordChanged confirms a password rotation after the fact, so a rotation
// the account holder did not perform is visible to them.
func PasswordChanged(l i18n.Locale) (mail.Message, error) {
	ctx := ctxFor(l)
	d := alertData{
		Heading: i18n.T(ctx, "email.password_changed_heading"),
		Intro:   i18n.T(ctx, "email.password_changed_intro"),
		Advice:  i18n.T(ctx, "email.password_changed_advice"),
	}
	return render(ctx, alertEmail(d), i18n.T(ctx, "email.password_changed_subject"),
		joinText(d.Intro, d.Advice))
}

// linkMessage builds any of the link-shaped mails from a catalog key prefix,
// so adding one is a catalog change rather than a new render path.
func linkMessage(l i18n.Locale, kind, link string, ttl time.Duration) (mail.Message, error) {
	ctx := ctxFor(l)
	d := linkData{
		Heading:       i18n.T(ctx, "email."+kind+"_heading"),
		Intro:         i18n.T(ctx, "email."+kind+"_intro"),
		CTA:           i18n.T(ctx, "email."+kind+"_cta"),
		Link:          link,
		FallbackIntro: i18n.T(ctx, "email.link_fallback"),
		Expiry:        expiryLine(ctx, ttl),
		Ignore:        i18n.T(ctx, "email.ignore_notice"),
	}
	text := joinText(d.Intro, link, d.Expiry+" "+d.Ignore)
	return render(ctx, linkEmail(d), i18n.T(ctx, "email."+kind+"_subject"), text)
}

// expiryLine renders the validity window in whole hours when it divides evenly,
// and minutes otherwise — "expires in 60 minutes" reads worse than "in 1 hour",
// and both forms need the locale's plural rules.
func expiryLine(ctx context.Context, ttl time.Duration) string {
	if ttl >= time.Hour && ttl%time.Hour == 0 {
		return i18n.Tn(ctx, "email.expires_in_hours", int(ttl/time.Hour))
	}
	return i18n.Tn(ctx, "email.expires_in_minutes", int(ttl.Minutes()))
}

// codeExpiryLine is expiryLine for a one-time code. Separate wording because a
// code email contains no link, and telling the reader "this link expires" sends
// them hunting for one that is not there.
func codeExpiryLine(ctx context.Context, ttl time.Duration) string {
	if ttl >= time.Hour && ttl%time.Hour == 0 {
		return i18n.Tn(ctx, "email.code_expires_in_hours", int(ttl/time.Hour))
	}
	return i18n.Tn(ctx, "email.code_expires_in_minutes", int(ttl.Minutes()))
}

// ctxFor makes a context carrying just the locale, which is all the i18n
// helpers need. Emails are composed outside any request.
func ctxFor(l i18n.Locale) context.Context {
	return i18n.WithLocale(context.Background(), l)
}

// joinText builds the plaintext half from the same translated strings the HTML
// uses. It is authored rather than converted from the HTML: an HTML-to-text
// converter is a permanent source of "the plain version says &nbsp;" bugs, and
// a transactional email's text alternative is only ever a few lines.
func joinText(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			kept = append(kept, s)
		}
	}
	return strings.Join(kept, "\n\n") + "\n"
}

func render(ctx context.Context, c templ.Component, subject, text string) (mail.Message, error) {
	var buf bytes.Buffer
	if err := c.Render(ctx, &buf); err != nil {
		return mail.Message{}, fmt.Errorf("render email: %w", err)
	}
	return mail.Message{Subject: subject, HTML: buf.String(), Text: text}, nil
}
