package views

import "context"

type ctxKey int

const (
	userEmailKey ctxKey = iota
	userNameKey
)

// WithUserEmail returns ctx carrying the authenticated user's email so the shared
// layout (sidebar user menu) can display it without every page threading it
// through. Set by the requireAuth middleware on the request context.
func WithUserEmail(ctx context.Context, email string) context.Context {
	return context.WithValue(ctx, userEmailKey, email)
}

// UserEmailFromContext returns the email set by WithUserEmail, or "" if absent.
func UserEmailFromContext(ctx context.Context) string {
	s, _ := ctx.Value(userEmailKey).(string)
	return s
}

// WithUserName returns ctx carrying the authenticated user's display name for
// the sidebar user menu. Set alongside the email by requireAuth.
func WithUserName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, userNameKey, name)
}

// UserNameFromContext returns the name set by WithUserName, or "" if absent.
func UserNameFromContext(ctx context.Context) string {
	s, _ := ctx.Value(userNameKey).(string)
	return s
}
