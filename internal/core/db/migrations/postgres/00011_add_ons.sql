-- +goose Up
-- Add-ons: an opt-in feature catalog (add_ons) plus a per-user linking table
-- (user_add_ons). A user enables an add-on once (a row is inserted) and toggles
-- it via the enabled column thereafter, so we never churn rows. price_cents is a
-- monthly price in integer cents (0 = free); Stripe/subscription wiring comes
-- later. No user_add_ons rows are seeded — every add-on starts opt-in (off).
-- +goose StatementBegin
CREATE TABLE add_ons (
    id          BIGSERIAL PRIMARY KEY,
    slug        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    price_cents INTEGER NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE user_add_ons (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    add_on_id  BIGINT NOT NULL REFERENCES add_ons(id) ON DELETE CASCADE,
    enabled    BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (user_id, add_on_id)
);
CREATE INDEX idx_user_add_ons_user ON user_add_ons(user_id);

INSERT INTO add_ons (slug, name, description, price_cents) VALUES
    ('paydown', 'Debt Paydown', 'Project payoff timelines and total interest across your debt accounts.', 0);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE user_add_ons;
DROP TABLE add_ons;
-- +goose StatementEnd
