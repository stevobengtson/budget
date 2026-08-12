-- +goose Up
-- onboarded_at records that the user finished the first-run wizard. NULL means
-- the wizard still has to run, so it is the single piece of state deciding
-- whether a signed-in user lands on /welcome or on their budget.
--
-- Explicit state rather than inferring "has no accounts": the wizard now seeds
-- the starter categories, so inferring would re-run it (and re-ask the language
-- question) for anyone who archived their last account or used the Danger
-- Zone's "start fresh".
--
-- Every existing user is backfilled: they signed up before the wizard existed
-- and are demonstrably set up, so none of them should be sent through it.
-- +goose StatementBegin
ALTER TABLE users ADD COLUMN onboarded_at TIMESTAMPTZ;
UPDATE users SET onboarded_at = CURRENT_TIMESTAMP;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN onboarded_at;
-- +goose StatementEnd
