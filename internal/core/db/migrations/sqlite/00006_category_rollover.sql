-- +goose Up
-- +goose StatementBegin
ALTER TABLE categories ADD COLUMN rollover_mode TEXT NOT NULL DEFAULT 'carry_positive'
    CHECK (rollover_mode IN ('none','carry','carry_positive'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE categories DROP COLUMN rollover_mode;
-- +goose StatementEnd
