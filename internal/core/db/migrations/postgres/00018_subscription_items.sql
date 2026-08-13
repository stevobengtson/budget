-- +goose Up
-- One store row per subscription *item* instead of per subscription. A single
-- Stripe subscription can now carry the base plan plus add-on items (e.g. the
-- bank_sync add-on), and each item projects to its own row so the access gate's
-- (user_id, price_id) lookups keep working unchanged. The old one-row-per-
-- subscription shape is the degenerate single-item case of the new key.
-- +goose StatementBegin
ALTER TABLE subscriptions DROP CONSTRAINT subscriptions_stripe_subscription_id_key;
ALTER TABLE subscriptions ADD CONSTRAINT subscriptions_sub_price_uq UNIQUE (stripe_subscription_id, price_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE subscriptions DROP CONSTRAINT subscriptions_sub_price_uq;
ALTER TABLE subscriptions ADD CONSTRAINT subscriptions_stripe_subscription_id_key UNIQUE (stripe_subscription_id);
-- +goose StatementEnd
