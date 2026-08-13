-- +goose Up
-- Budget Estimate add-on: a named snapshot of the user's budget (groups,
-- categories, assigned amounts) and a month's income rows, editable without
-- touching the real budget. Rows are pure copies — no foreign keys back to
-- categories/incomes, so the originals stay freely editable and deletable.
-- user_id is denormalized onto every table so all writes use the store's
-- standard `AND user_id=$n` ownership scoping.
-- +goose StatementBegin
CREATE TABLE estimates (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_estimates_user ON estimates(user_id);

CREATE TABLE estimate_groups (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    estimate_id BIGINT NOT NULL REFERENCES estimates(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    sort_order  BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX idx_estimate_groups_estimate ON estimate_groups(estimate_id);

CREATE TABLE estimate_categories (
    id             BIGSERIAL PRIMARY KEY,
    user_id        BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id       BIGINT NOT NULL REFERENCES estimate_groups(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    assigned_cents BIGINT NOT NULL DEFAULT 0,
    sort_order     BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX idx_estimate_categories_group ON estimate_categories(group_id);

CREATE TABLE estimate_incomes (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    estimate_id  BIGINT NOT NULL REFERENCES estimates(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    amount_cents BIGINT NOT NULL DEFAULT 0,
    sort_order   BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX idx_estimate_incomes_estimate ON estimate_incomes(estimate_id);

INSERT INTO add_ons (slug, name, description, price_cents) VALUES
    ('estimates', 'Budget Estimate', 'Snapshot your budget and income, then adjust the copy to plan changes without touching the real thing.', 0);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE estimate_incomes;
DROP TABLE estimate_categories;
DROP TABLE estimate_groups;
DROP TABLE estimates;
DELETE FROM user_add_ons WHERE add_on_id = (SELECT id FROM add_ons WHERE slug = 'estimates');
DELETE FROM add_ons WHERE slug = 'estimates';
-- +goose StatementEnd
