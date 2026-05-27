-- +goose Up
-- +goose StatementBegin
ALTER TABLE accounts ADD COLUMN payment_category_id BIGINT REFERENCES categories(id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE accounts DROP COLUMN payment_category_id;
-- +goose StatementEnd
