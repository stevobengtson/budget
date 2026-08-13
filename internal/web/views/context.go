package views

import "context"

type ctxKey int

const (
	userEmailKey ctxKey = iota
	userNameKey
	userAvatarVersionKey
	enabledAddOnsKey
	isAdminKey
	reviewCountKey
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

// WithUserAvatarVersion returns ctx carrying the user's avatar cache-busting
// version (0 when there's no custom avatar) so the sidebar can render the right
// image or fall back to the monogram.
func WithUserAvatarVersion(ctx context.Context, version int64) context.Context {
	return context.WithValue(ctx, userAvatarVersionKey, version)
}

// UserAvatarVersionFromContext returns the version set by WithUserAvatarVersion,
// or 0 if absent (render the monogram fallback).
func UserAvatarVersionFromContext(ctx context.Context) int64 {
	v, _ := ctx.Value(userAvatarVersionKey).(int64)
	return v
}

// WithEnabledAddOns returns ctx carrying the slugs of the add-ons the user has
// enabled, so the shared layout can gate add-on-owned navigation. Set by the
// requireAuth middleware alongside the other user fields.
func WithEnabledAddOns(ctx context.Context, slugs []string) context.Context {
	return context.WithValue(ctx, enabledAddOnsKey, slugs)
}

// WithIsAdmin returns ctx carrying whether the authenticated user is an admin,
// so the shared user menu can show the Admin entry. Set by requireAuth. This is
// display only — the /admin routes are guarded server-side by requireAdmin.
func WithIsAdmin(ctx context.Context, admin bool) context.Context {
	return context.WithValue(ctx, isAdminKey, admin)
}

// IsAdmin reports whether the request's user is an admin. False when absent.
func IsAdmin(ctx context.Context) bool {
	v, _ := ctx.Value(isAdminKey).(bool)
	return v
}

// WithReviewCount returns ctx carrying the user's count of unreviewed
// bank-sync imports, for the shared layout's Activity badge. Set by
// requireAuth only when the bank_sync add-on is enabled; absent reads as 0.
func WithReviewCount(ctx context.Context, n int64) context.Context {
	return context.WithValue(ctx, reviewCountKey, n)
}

// ReviewCountFrom returns the count set by WithReviewCount, or 0 if absent.
func ReviewCountFrom(ctx context.Context) int64 {
	v, _ := ctx.Value(reviewCountKey).(int64)
	return v
}

// AddOnEnabled reports whether the given add-on slug is in the enabled set on
// ctx (set by WithEnabledAddOns). False when absent.
func AddOnEnabled(ctx context.Context, slug string) bool {
	slugs, _ := ctx.Value(enabledAddOnsKey).([]string)
	for _, s := range slugs {
		if s == slug {
			return true
		}
	}
	return false
}
