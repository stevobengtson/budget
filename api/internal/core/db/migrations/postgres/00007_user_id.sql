-- +goose Up
-- +goose StatementBegin
-- Multi-user support for the API database. user_id is a native uuid matching
-- BetterAuth's user.id (the JWT `sub`). The DEFAULT is the LocalUserID sentinel
-- so the column backfills cleanly; the API always sets user_id explicitly.
ALTER TABLE accounts        ADD COLUMN user_id uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001';
ALTER TABLE category_groups ADD COLUMN user_id uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001';
ALTER TABLE categories      ADD COLUMN user_id uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001';
ALTER TABLE transactions    ADD COLUMN user_id uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001';
ALTER TABLE budgets         ADD COLUMN user_id uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001';
ALTER TABLE incomes         ADD COLUMN user_id uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001';
ALTER TABLE app_settings    ADD COLUMN user_id uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001';

-- Re-scope uniqueness to the owning user. (categories' UNIQUE(group_id, name)
-- and budgets' PK(month, category_id) stay valid because group_id/category_id
-- are globally unique, hence implicitly user-scoped.)
ALTER TABLE accounts        DROP CONSTRAINT IF EXISTS accounts_name_key;
ALTER TABLE accounts        ADD CONSTRAINT accounts_user_name_key UNIQUE (user_id, name);

ALTER TABLE category_groups DROP CONSTRAINT IF EXISTS category_groups_name_key;
ALTER TABLE category_groups ADD CONSTRAINT category_groups_user_name_key UNIQUE (user_id, name);

ALTER TABLE incomes         DROP CONSTRAINT IF EXISTS incomes_month_name_key;
ALTER TABLE incomes         ADD CONSTRAINT incomes_user_month_name_key UNIQUE (user_id, month, name);

ALTER TABLE app_settings    DROP CONSTRAINT IF EXISTS app_settings_pkey;
ALTER TABLE app_settings    ADD PRIMARY KEY (user_id, key);

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

ALTER TABLE app_settings    DROP CONSTRAINT IF EXISTS app_settings_pkey;
ALTER TABLE app_settings    ADD PRIMARY KEY (key);
ALTER TABLE incomes         DROP CONSTRAINT IF EXISTS incomes_user_month_name_key;
ALTER TABLE incomes         ADD CONSTRAINT incomes_month_name_key UNIQUE (month, name);
ALTER TABLE category_groups DROP CONSTRAINT IF EXISTS category_groups_user_name_key;
ALTER TABLE category_groups ADD CONSTRAINT category_groups_name_key UNIQUE (name);
ALTER TABLE accounts        DROP CONSTRAINT IF EXISTS accounts_user_name_key;
ALTER TABLE accounts        ADD CONSTRAINT accounts_name_key UNIQUE (name);

ALTER TABLE accounts        DROP COLUMN user_id;
ALTER TABLE category_groups DROP COLUMN user_id;
ALTER TABLE categories      DROP COLUMN user_id;
ALTER TABLE transactions    DROP COLUMN user_id;
ALTER TABLE budgets         DROP COLUMN user_id;
ALTER TABLE incomes         DROP COLUMN user_id;
ALTER TABLE app_settings    DROP COLUMN user_id;
-- +goose StatementEnd
