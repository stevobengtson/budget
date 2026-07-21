-- +goose Up
-- Transitional state: pre-existing rows get user_id = NULL, so the old global
-- name-uniqueness is intentionally not enforced for this legacy data until a
-- later task backfills user_id.
-- +goose StatementBegin
CREATE TABLE users (
    id                BIGSERIAL PRIMARY KEY,
    email             TEXT NOT NULL UNIQUE,
    password_hash     TEXT NOT NULL,
    email_verified_at TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE sessions (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    user_agent TEXT,
    ip         TEXT
);
CREATE INDEX idx_sessions_user ON sessions(user_id);

CREATE TABLE verification_tokens (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    purpose     TEXT NOT NULL CHECK (purpose IN ('email_verify','password_reset')),
    expires_at  TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_vtokens_user ON verification_tokens(user_id);

ALTER TABLE accounts        ADD COLUMN user_id BIGINT REFERENCES users(id);
ALTER TABLE category_groups ADD COLUMN user_id BIGINT REFERENCES users(id);
ALTER TABLE categories      ADD COLUMN user_id BIGINT REFERENCES users(id);
ALTER TABLE transactions    ADD COLUMN user_id BIGINT REFERENCES users(id);
ALTER TABLE budgets         ADD COLUMN user_id BIGINT REFERENCES users(id);
ALTER TABLE incomes         ADD COLUMN user_id BIGINT REFERENCES users(id);

ALTER TABLE accounts        DROP CONSTRAINT accounts_name_key;
ALTER TABLE accounts        ADD  CONSTRAINT accounts_user_name_key UNIQUE (user_id, name);
ALTER TABLE category_groups DROP CONSTRAINT category_groups_name_key;
ALTER TABLE category_groups ADD  CONSTRAINT category_groups_user_name_key UNIQUE (user_id, name);
ALTER TABLE incomes         DROP CONSTRAINT incomes_month_name_key;
ALTER TABLE incomes         ADD  CONSTRAINT incomes_user_month_name_key UNIQUE (user_id, month, name);

CREATE INDEX idx_accounts_user        ON accounts(user_id);
CREATE INDEX idx_category_groups_user ON category_groups(user_id);
CREATE INDEX idx_categories_user      ON categories(user_id);
CREATE INDEX idx_transactions_user    ON transactions(user_id);
CREATE INDEX idx_budgets_user         ON budgets(user_id);
CREATE INDEX idx_incomes_user         ON incomes(user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_incomes_user;
DROP INDEX IF EXISTS idx_budgets_user;
DROP INDEX IF EXISTS idx_transactions_user;
DROP INDEX IF EXISTS idx_categories_user;
DROP INDEX IF EXISTS idx_category_groups_user;
DROP INDEX IF EXISTS idx_accounts_user;

ALTER TABLE incomes         DROP CONSTRAINT incomes_user_month_name_key;
ALTER TABLE incomes         ADD  CONSTRAINT incomes_month_name_key UNIQUE (month, name);
ALTER TABLE category_groups DROP CONSTRAINT category_groups_user_name_key;
ALTER TABLE category_groups ADD  CONSTRAINT category_groups_name_key UNIQUE (name);
ALTER TABLE accounts        DROP CONSTRAINT accounts_user_name_key;
ALTER TABLE accounts        ADD  CONSTRAINT accounts_name_key UNIQUE (name);

ALTER TABLE incomes         DROP COLUMN user_id;
ALTER TABLE budgets         DROP COLUMN user_id;
ALTER TABLE transactions    DROP COLUMN user_id;
ALTER TABLE categories      DROP COLUMN user_id;
ALTER TABLE category_groups DROP COLUMN user_id;
ALTER TABLE accounts        DROP COLUMN user_id;

DROP TABLE verification_tokens;
DROP TABLE sessions;
DROP TABLE users;
-- +goose StatementEnd
