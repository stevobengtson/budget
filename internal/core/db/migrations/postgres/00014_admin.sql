-- +goose Up
-- Admin console support.
--
-- is_admin grants access to /admin. There is deliberately no UI to set it — an
-- admin cannot mint another admin through the app — so it is set manually:
--   UPDATE users SET is_admin = true WHERE email = 'you@example.com';
--
-- disabled_at suspends an account (abuse handling). A disabled user cannot log
-- in and any existing session is rejected; the row and its data are untouched,
-- so the action is reversible.
--
-- billing_exempt_until layers an expiry onto the existing billing_exempt flag:
-- exempt is the switch, until is the optional end date. NULL means no expiry,
-- so rows set before this migration (exempt = true, until NULL) keep behaving
-- as lifetime comps. See store.IsBillingExempt for the combined rule.
-- +goose StatementBegin
ALTER TABLE users ADD COLUMN is_admin BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN disabled_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN billing_exempt_until TIMESTAMPTZ;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN billing_exempt_until;
ALTER TABLE users DROP COLUMN disabled_at;
ALTER TABLE users DROP COLUMN is_admin;
-- +goose StatementEnd
