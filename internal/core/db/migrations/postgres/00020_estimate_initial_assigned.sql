-- +goose Up
-- Repair: 00017 was edited after production had already applied it — the
-- initial_assigned_cents column was added to the CREATE TABLE in a later
-- commit, so databases that ran the original 00017 (production) lack the
-- column while databases migrated after the edit (dev/test) have it. Goose
-- tracks versions, not file contents, so the edit silently never reached
-- production. IF NOT EXISTS converges both states; the DEFAULT matches the
-- edited 00017 exactly.
--
-- Lesson encoded here: never edit an applied migration — add a new one.
-- +goose StatementBegin
ALTER TABLE estimate_categories ADD COLUMN IF NOT EXISTS initial_assigned_cents BIGINT NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- No-op: on databases whose 00017 created the column, dropping it here would
-- desync the 00017 rollback; on repaired databases the column is legitimate
-- schema. Either way the column stays.
SELECT 1;
-- +goose StatementEnd
