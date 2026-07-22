-- +goose Up
-- billing_exempt marks a complimentary / always-free account that bypasses the
-- subscription gate entirely (no trial, no Stripe required). Set manually in the
-- database, e.g. the owner account:
--   UPDATE users SET billing_exempt = true WHERE email = 'you@example.com';
-- +goose StatementBegin
ALTER TABLE users ADD COLUMN billing_exempt BOOLEAN NOT NULL DEFAULT FALSE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN billing_exempt;
-- +goose StatementEnd
