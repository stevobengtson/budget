-- +goose Up
-- +goose StatementBegin
CREATE TABLE accounts (
    id                     BIGSERIAL PRIMARY KEY,
    name                   TEXT NOT NULL UNIQUE,
    type                   TEXT NOT NULL CHECK (type IN ('checking','savings','cash','credit','loan')),
    starting_balance_cents BIGINT NOT NULL DEFAULT 0,
    credit_limit_cents     BIGINT,
    apr_bps                BIGINT,
    archived_at            TIMESTAMPTZ,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE category_groups (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    sort_order BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE categories (
    id            BIGSERIAL PRIMARY KEY,
    group_id      BIGINT NOT NULL REFERENCES category_groups(id),
    name          TEXT NOT NULL,
    goal_cents    BIGINT,
    goal_due_date DATE,
    sort_order    BIGINT NOT NULL DEFAULT 0,
    archived_at   TIMESTAMPTZ,
    UNIQUE (group_id, name)
);

CREATE TABLE transactions (
    id                  BIGSERIAL PRIMARY KEY,
    date                DATE NOT NULL,
    account_id          BIGINT NOT NULL REFERENCES accounts(id),
    category_id         BIGINT REFERENCES categories(id),
    transfer_account_id BIGINT REFERENCES accounts(id),
    transfer_pair_id    BIGINT REFERENCES transactions(id),
    payee               TEXT,
    notes               TEXT,
    outflow_cents       BIGINT NOT NULL DEFAULT 0,
    inflow_cents        BIGINT NOT NULL DEFAULT 0,
    cleared             BOOLEAN NOT NULL DEFAULT FALSE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (outflow_cents >= 0 AND inflow_cents >= 0),
    CHECK (outflow_cents = 0 OR inflow_cents = 0)
);

CREATE INDEX idx_tx_account_date  ON transactions(account_id, date);
CREATE INDEX idx_tx_category_date ON transactions(category_id, date);

CREATE TABLE budgets (
    month          TEXT NOT NULL,
    category_id    BIGINT NOT NULL REFERENCES categories(id),
    assigned_cents BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (month, category_id)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE budgets;
DROP TABLE transactions;
DROP TABLE categories;
DROP TABLE category_groups;
DROP TABLE accounts;
-- +goose StatementEnd
