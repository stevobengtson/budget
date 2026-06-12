-- +goose Up
-- +goose StatementBegin
CREATE TABLE incomes (
    id           INTEGER PRIMARY KEY,
    month        TEXT NOT NULL,                 -- 'YYYY-MM'
    name         TEXT NOT NULL,
    amount_cents INTEGER NOT NULL DEFAULT 0,
    sort_order   INTEGER NOT NULL DEFAULT 0,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (month, name)
);
CREATE INDEX idx_incomes_month ON incomes(month);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE incomes;
-- +goose StatementEnd
