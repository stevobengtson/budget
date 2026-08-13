-- +goose Up
-- Bank Sync add-on (Plaid): plaid_items holds one row per linked bank login
-- (Plaid "Item") with the access token encrypted at the application layer
-- (AES-256-GCM; the key never touches the database). Accounts opt in by
-- pointing at an item + a Plaid account id; plaid_sync_from bounds imports so
-- linking an existing account doesn't double-count history against the derived
-- balance. Transactions gain the Plaid id (the sync's idempotency key) and a
-- needs_review flag surfacing imports the user hasn't looked at yet.
--
-- plaid_items.user_id has no ON DELETE CASCADE on purpose: account deletion
-- must first revoke the token at Plaid (/item/remove), so deletes go through
-- the store's ordered wipe, not the database.
-- +goose StatementBegin
CREATE TABLE plaid_items (
    id                     BIGSERIAL PRIMARY KEY,
    user_id                BIGINT NOT NULL REFERENCES users(id),
    plaid_item_id          TEXT NOT NULL UNIQUE,
    access_token_encrypted BYTEA NOT NULL,
    institution_id         TEXT NOT NULL DEFAULT '',
    institution_name       TEXT NOT NULL DEFAULT '',
    sync_cursor            TEXT NOT NULL DEFAULT '',
    status                 TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'login_required', 'pending_expiration', 'error')),
    last_error             TEXT NOT NULL DEFAULT '',
    last_synced_at         TIMESTAMPTZ,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_plaid_items_user ON plaid_items(user_id);

ALTER TABLE accounts ADD COLUMN plaid_item_id    BIGINT REFERENCES plaid_items(id);
ALTER TABLE accounts ADD COLUMN plaid_account_id TEXT;
ALTER TABLE accounts ADD COLUMN plaid_sync_from  DATE;
CREATE UNIQUE INDEX idx_accounts_plaid_account
    ON accounts(plaid_item_id, plaid_account_id) WHERE plaid_account_id IS NOT NULL;

ALTER TABLE transactions ADD COLUMN plaid_transaction_id TEXT;
ALTER TABLE transactions ADD COLUMN needs_review BOOLEAN NOT NULL DEFAULT FALSE;
CREATE UNIQUE INDEX idx_tx_plaid_id ON transactions(plaid_transaction_id)
    WHERE plaid_transaction_id IS NOT NULL;
CREATE INDEX idx_tx_needs_review ON transactions(user_id) WHERE needs_review;

INSERT INTO add_ons (slug, name, description, price_cents) VALUES
    ('bank_sync', 'Bank Sync', 'Connect your bank through Plaid and import transactions automatically.', 199);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_tx_needs_review;
DROP INDEX idx_tx_plaid_id;
ALTER TABLE transactions DROP COLUMN needs_review;
ALTER TABLE transactions DROP COLUMN plaid_transaction_id;
DROP INDEX idx_accounts_plaid_account;
ALTER TABLE accounts DROP COLUMN plaid_sync_from;
ALTER TABLE accounts DROP COLUMN plaid_account_id;
ALTER TABLE accounts DROP COLUMN plaid_item_id;
DROP TABLE plaid_items;
DELETE FROM user_add_ons WHERE add_on_id = (SELECT id FROM add_ons WHERE slug = 'bank_sync');
DELETE FROM add_ons WHERE slug = 'bank_sync';
-- +goose StatementEnd
