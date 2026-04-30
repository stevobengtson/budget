-- +goose Up
-- +goose StatementBegin
CREATE TABLE accounts (
    id                     INTEGER PRIMARY KEY,
    name                   TEXT NOT NULL UNIQUE,
    type                   TEXT NOT NULL CHECK (type IN ('checking','savings','cash','credit','loan')),
    starting_balance_cents INTEGER NOT NULL DEFAULT 0,
    credit_limit_cents     INTEGER,
    apr_bps                INTEGER,
    archived_at            DATETIME,
    created_at             DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE category_groups (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    sort_order INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE categories (
    id            INTEGER PRIMARY KEY,
    group_id      INTEGER NOT NULL REFERENCES category_groups(id),
    name          TEXT NOT NULL,
    goal_cents    INTEGER,
    goal_due_date DATE,
    sort_order    INTEGER NOT NULL DEFAULT 0,
    archived_at   DATETIME,
    UNIQUE (group_id, name)
);

CREATE TABLE transactions (
    id                  INTEGER PRIMARY KEY,
    date                DATE NOT NULL,
    account_id          INTEGER NOT NULL REFERENCES accounts(id),
    category_id         INTEGER REFERENCES categories(id),
    transfer_account_id INTEGER REFERENCES accounts(id),
    transfer_pair_id    INTEGER REFERENCES transactions(id),
    payee               TEXT,
    notes               TEXT,
    outflow_cents       INTEGER NOT NULL DEFAULT 0,
    inflow_cents        INTEGER NOT NULL DEFAULT 0,
    cleared             BOOLEAN NOT NULL DEFAULT 0,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (outflow_cents >= 0 AND inflow_cents >= 0),
    CHECK (outflow_cents = 0 OR inflow_cents = 0)
);

CREATE INDEX idx_tx_account_date  ON transactions(account_id, date);
CREATE INDEX idx_tx_category_date ON transactions(category_id, date);

CREATE TABLE budgets (
    month          TEXT NOT NULL,
    category_id    INTEGER NOT NULL REFERENCES categories(id),
    assigned_cents INTEGER NOT NULL DEFAULT 0,
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
