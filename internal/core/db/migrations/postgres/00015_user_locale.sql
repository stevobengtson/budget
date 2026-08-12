-- +goose Up
-- locale is the user's chosen interface language as a BCP 47 tag ("en-CA",
-- "fr-CA"). Existing accounts default to Canadian English, which is what they
-- have been seeing.
--
-- Deliberately a plain TEXT rather than an enum: the supported set lives in
-- internal/core/i18n, which validates on the way in and falls back to the
-- default on the way out, so adding a language does not need a migration. A
-- stale value left behind by removing one degrades to the default instead of
-- breaking the login.
-- +goose StatementBegin
ALTER TABLE users ADD COLUMN locale TEXT NOT NULL DEFAULT 'en-CA';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN locale;
-- +goose StatementEnd
