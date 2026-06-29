import {
	bigint,
	bigserial,
	boolean,
	date,
	index,
	pgTable,
	primaryKey,
	text,
	timestamp,
	unique,
	uuid,
} from "drizzle-orm/pg-core";

export type AccountType = "checking" | "savings" | "cash" | "credit" | "loan";

const userId = () =>
	uuid("user_id").notNull().default("00000000-0000-0000-0000-000000000001");

export const accounts = pgTable(
	"accounts",
	{
		id: bigserial("id", { mode: "number" }).primaryKey(),
		userId: userId(),
		name: text("name").notNull(),
		// checking|savings|cash|credit|loan. `.$type` is compile-time only; the
		// Go migration had a DB CHECK constraint — add one here in Plan 2.
		type: text("type").$type<AccountType>().notNull(),
		startingBalanceCents: bigint("starting_balance_cents", { mode: "number" })
			.notNull()
			.default(0),
		creditLimitCents: bigint("credit_limit_cents", { mode: "number" }),
		aprBps: bigint("apr_bps", { mode: "number" }),
		monthlyPaymentCents: bigint("monthly_payment_cents", { mode: "number" }),
		includeInPaydown: boolean("include_in_paydown").notNull().default(false),
		paymentCategoryId: bigint("payment_category_id", { mode: "number" }),
		archivedAt: timestamp("archived_at", { withTimezone: true }),
		createdAt: timestamp("created_at", { withTimezone: true })
			.notNull()
			.defaultNow(),
	},
	(t) => [
		unique("accounts_user_name_key").on(t.userId, t.name),
		index("idx_accounts_user").on(t.userId),
	],
);

export const categoryGroups = pgTable(
	"category_groups",
	{
		id: bigserial("id", { mode: "number" }).primaryKey(),
		userId: userId(),
		name: text("name").notNull(),
		sortOrder: bigint("sort_order", { mode: "number" }).notNull().default(0),
	},
	(t) => [
		unique("category_groups_user_name_key").on(t.userId, t.name),
		index("idx_category_groups_user").on(t.userId),
	],
);

export const categories = pgTable(
	"categories",
	{
		id: bigserial("id", { mode: "number" }).primaryKey(),
		userId: userId(),
		groupId: bigint("group_id", { mode: "number" })
			.notNull()
			.references(() => categoryGroups.id),
		name: text("name").notNull(),
		goalCents: bigint("goal_cents", { mode: "number" }),
		goalDueDate: date("goal_due_date"),
		sortOrder: bigint("sort_order", { mode: "number" }).notNull().default(0),
		isIncome: boolean("is_income").notNull().default(false),
		archivedAt: timestamp("archived_at", { withTimezone: true }),
	},
	(t) => [
		unique("categories_group_name_key").on(t.groupId, t.name),
		index("idx_categories_user").on(t.userId),
	],
);

export const transactions = pgTable(
	"transactions",
	{
		id: bigserial("id", { mode: "number" }).primaryKey(),
		userId: userId(),
		date: date("date").notNull(),
		accountId: bigint("account_id", { mode: "number" })
			.notNull()
			.references(() => accounts.id),
		categoryId: bigint("category_id", { mode: "number" }).references(
			() => categories.id,
		),
		transferAccountId: bigint("transfer_account_id", {
			mode: "number",
		}).references(() => accounts.id),
		transferPairId: bigint("transfer_pair_id", { mode: "number" }),
		payee: text("payee"),
		notes: text("notes"),
		outflowCents: bigint("outflow_cents", { mode: "number" })
			.notNull()
			.default(0),
		inflowCents: bigint("inflow_cents", { mode: "number" })
			.notNull()
			.default(0),
		cleared: boolean("cleared").notNull().default(false),
		createdAt: timestamp("created_at", { withTimezone: true })
			.notNull()
			.defaultNow(),
	},
	(t) => [
		index("idx_tx_account_date").on(t.accountId, t.date),
		index("idx_tx_category_date").on(t.categoryId, t.date),
		index("idx_transactions_user").on(t.userId),
	],
);

export const budgets = pgTable(
	"budgets",
	{
		userId: userId(),
		month: text("month").notNull(), // "YYYY-MM"
		categoryId: bigint("category_id", { mode: "number" })
			.notNull()
			.references(() => categories.id),
		assignedCents: bigint("assigned_cents", { mode: "number" })
			.notNull()
			.default(0),
	},
	(t) => [
		primaryKey({ columns: [t.month, t.categoryId] }),
		index("idx_budgets_user").on(t.userId),
	],
);

export const incomes = pgTable(
	"incomes",
	{
		id: bigserial("id", { mode: "number" }).primaryKey(),
		userId: userId(),
		month: text("month").notNull(),
		name: text("name").notNull(),
		amountCents: bigint("amount_cents", { mode: "number" })
			.notNull()
			.default(0),
		sortOrder: bigint("sort_order", { mode: "number" }).notNull().default(0),
		createdAt: timestamp("created_at", { withTimezone: true })
			.notNull()
			.defaultNow(),
	},
	(t) => [
		unique("incomes_user_month_name_key").on(t.userId, t.month, t.name),
		index("idx_incomes_user").on(t.userId),
		index("idx_incomes_month").on(t.month),
	],
);

export const appSettings = pgTable(
	"app_settings",
	{
		userId: userId(),
		key: text("key").notNull(),
		value: text("value").notNull(),
	},
	(t) => [primaryKey({ columns: [t.userId, t.key] })],
);
