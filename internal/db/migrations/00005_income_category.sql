-- +goose Up
-- +goose StatementBegin
ALTER TABLE categories ADD COLUMN is_income BOOLEAN NOT NULL DEFAULT 0;

-- Seed an "Income" group + a system "Income" category so the user can
-- categorize paycheck inflows. The category is locked from edit/delete by
-- the TUI when is_income = 1.
INSERT INTO category_groups(name, sort_order)
SELECT 'Income', -100
WHERE NOT EXISTS (SELECT 1 FROM category_groups WHERE name = 'Income');

INSERT INTO categories(group_id, name, is_income, sort_order)
SELECT g.id, 'Income', 1, 0
FROM category_groups g
WHERE g.name = 'Income'
  AND NOT EXISTS (SELECT 1 FROM categories WHERE name = 'Income' AND is_income = 1);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM categories WHERE is_income = 1;
DELETE FROM category_groups WHERE name = 'Income' AND NOT EXISTS (
  SELECT 1 FROM categories WHERE group_id = category_groups.id
);
ALTER TABLE categories DROP COLUMN is_income;
-- +goose StatementEnd
