-- +goose Up
-- +goose StatementBegin
CREATE TABLE incomes (
    id           BIGSERIAL PRIMARY KEY,
    month        TEXT NOT NULL,
    name         TEXT NOT NULL,
    amount_cents BIGINT NOT NULL DEFAULT 0,
    sort_order   BIGINT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (month, name)
);
CREATE INDEX idx_incomes_month ON incomes(month);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE incomes;
-- +goose StatementEnd
