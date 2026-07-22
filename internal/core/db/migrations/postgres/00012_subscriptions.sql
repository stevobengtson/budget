-- +goose Up
-- Stripe billing: link each user to a Stripe customer and track their
-- subscriptions. One row per Stripe subscription; the base $4.99/mo plan is
-- matched by price_id (config stripe.price_ids.base). status mirrors Stripe's
-- subscription status (trialing, active, past_due, canceled, unpaid, paused,
-- ...) verbatim — no CHECK, since Stripe may add values. Currency is captured
-- from the subscription (Checkout picks it by the buyer's IP). trial_end and
-- current_period_end come from Stripe and drive the app's access gate.
-- +goose StatementBegin
ALTER TABLE users ADD COLUMN stripe_customer_id TEXT UNIQUE;

CREATE TABLE subscriptions (
    id                     BIGSERIAL PRIMARY KEY,
    user_id                BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    stripe_subscription_id TEXT NOT NULL UNIQUE,
    stripe_customer_id     TEXT NOT NULL,
    price_id               TEXT NOT NULL,
    status                 TEXT NOT NULL,
    currency               TEXT NOT NULL DEFAULT '',
    trial_end              TIMESTAMPTZ,
    current_period_end     TIMESTAMPTZ,
    cancel_at_period_end   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_subscriptions_user ON subscriptions(user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE subscriptions;
ALTER TABLE users DROP COLUMN stripe_customer_id;
-- +goose StatementEnd
