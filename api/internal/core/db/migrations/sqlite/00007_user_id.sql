-- +goose Up
-- +goose StatementBegin
-- Multi-user support. SQLite has no native uuid type, so user_id is TEXT and
-- stores the UUID string. The local single-user TUI relies on the DEFAULT
-- (the LocalUserID sentinel in the store package), so existing inserts that
-- omit user_id keep working. Original UNIQUE/PK constraints stay as-is — with
-- one local user there are no cross-user collisions.
ALTER TABLE accounts        ADD COLUMN user_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001';
ALTER TABLE category_groups ADD COLUMN user_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001';
ALTER TABLE categories      ADD COLUMN user_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001';
ALTER TABLE transactions    ADD COLUMN user_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001';
ALTER TABLE budgets         ADD COLUMN user_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001';
ALTER TABLE incomes         ADD COLUMN user_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001';
ALTER TABLE app_settings    ADD COLUMN user_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001';

CREATE INDEX idx_accounts_user        ON accounts(user_id);
CREATE INDEX idx_category_groups_user ON category_groups(user_id);
CREATE INDEX idx_categories_user      ON categories(user_id);
CREATE INDEX idx_transactions_user    ON transactions(user_id);
CREATE INDEX idx_budgets_user         ON budgets(user_id);
CREATE INDEX idx_incomes_user         ON incomes(user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_accounts_user;
DROP INDEX IF EXISTS idx_category_groups_user;
DROP INDEX IF EXISTS idx_categories_user;
DROP INDEX IF EXISTS idx_transactions_user;
DROP INDEX IF EXISTS idx_budgets_user;
DROP INDEX IF EXISTS idx_incomes_user;

ALTER TABLE accounts        DROP COLUMN user_id;
ALTER TABLE category_groups DROP COLUMN user_id;
ALTER TABLE categories      DROP COLUMN user_id;
ALTER TABLE transactions    DROP COLUMN user_id;
ALTER TABLE budgets         DROP COLUMN user_id;
ALTER TABLE incomes         DROP COLUMN user_id;
ALTER TABLE app_settings    DROP COLUMN user_id;
-- +goose StatementEnd
