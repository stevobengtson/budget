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
