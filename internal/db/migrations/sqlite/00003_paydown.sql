-- +goose Up
-- +goose StatementBegin
ALTER TABLE accounts ADD COLUMN monthly_payment_cents INTEGER;
ALTER TABLE accounts ADD COLUMN include_in_paydown   BOOLEAN NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE accounts DROP COLUMN include_in_paydown;
ALTER TABLE accounts DROP COLUMN monthly_payment_cents;
-- +goose StatementEnd
