-- +goose Up
-- +goose StatementBegin
ALTER TABLE users ADD COLUMN avatar BYTEA;
ALTER TABLE users ADD COLUMN avatar_updated_at TIMESTAMPTZ;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN avatar;
ALTER TABLE users DROP COLUMN avatar_updated_at;
-- +goose StatementEnd
