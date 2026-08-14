-- +goose Up
-- +goose StatementBegin

-- Magic links need no table of their own.
--
-- A link and an emailed one-time code are the same possession factor — "prove
-- you can read this inbox" — so they ride the same auth_challenges row, which
-- already carries a hashed code, an attempt counter, single-use consumption and
-- an expiry. Building a second, near-identical table would mean two places to
-- get the expiry, the attempt cap and the anti-enumeration response wrong.
--
-- The only difference from an MFA challenge is when the row is created: a magic
-- link opens one BEFORE any factor has been proved, so it is a first factor
-- rather than a second. Hence a distinct kind.
ALTER TABLE auth_challenges DROP CONSTRAINT auth_challenges_kind_check;
ALTER TABLE auth_challenges ADD CONSTRAINT auth_challenges_kind_check
    CHECK (kind IN ('mfa','webauthn_login','webauthn_register','login'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM auth_challenges WHERE kind = 'login';
ALTER TABLE auth_challenges DROP CONSTRAINT auth_challenges_kind_check;
ALTER TABLE auth_challenges ADD CONSTRAINT auth_challenges_kind_check
    CHECK (kind IN ('mfa','webauthn_login','webauthn_register'));
-- +goose StatementEnd
