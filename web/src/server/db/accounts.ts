import { and, asc, eq, isNull, sql } from "drizzle-orm";
import { type AccountType, accounts } from "../../db/schema/budget-schema.ts";
import { getDb } from "../../lib/db.ts";

export type { AccountType };

export type Account = {
	id: number;
	name: string;
	type: AccountType;
	startingBalanceCents: number;
	creditLimitCents: number | null;
	aprBps: number | null;
	monthlyPaymentCents: number | null;
	includeInPaydown: boolean;
	paymentCategoryId: number | null;
	archivedAt: Date | null;
	createdAt: Date;
};

export type AccountWithBalance = Account & { balanceCents: number };

export type CreateAccountInput = {
	name: string;
	type: AccountType;
	startingBalanceCents: number;
	creditLimitCents?: number | null;
	aprBps?: number | null;
	monthlyPaymentCents?: number | null;
	includeInPaydown: boolean;
	paymentCategoryId?: number | null;
};

// IMPORTANT: the outer query MUST use `.from(accounts)` with NO alias. The raw
// `accounts.id` / `accounts.user_id` identifiers below correlate to the outer
// accounts row by table name; if the outer FROM aliases the table, these
// references silently bind wrong (or error) and balances break. When copying
// this pattern to other domains, keep the unaliased outer FROM.
//
// balanceExpr mirrors the Go SQL: starting balance plus the user's inflows
// minus outflows on this account.
//
// NOTE: The correlated subquery uses raw SQL identifiers `accounts.id` /
// `accounts.user_id` rather than Drizzle column interpolations
// (`${accounts.id}`).  When a Drizzle column object is interpolated into a
// sql`` template it emits an unqualified name (e.g. `"id"`).  Inside the
// subquery that name is resolved against the inner FROM clause first, so
// PostgreSQL binds it to `t.id` (the transaction PK) instead of the outer
// `accounts.id`.  Using the raw table-qualified identifier avoids that
// ambiguity while still working as a proper correlated subquery.
const balanceExpr = sql<number>`
	${accounts.startingBalanceCents}
	-- inflow and outflow are intentionally separate subqueries (mirroring the Go
	-- SQL) to keep the +inflow / -outflow sign logic explicit.
	+ COALESCE((SELECT SUM(t.inflow_cents)  FROM transactions t
	            WHERE t.account_id = accounts.id AND t.user_id = accounts.user_id), 0)
	- COALESCE((SELECT SUM(t.outflow_cents) FROM transactions t
	            WHERE t.account_id = accounts.id AND t.user_id = accounts.user_id), 0)
`.mapWith(Number);

const accountColumns = {
	id: accounts.id,
	name: accounts.name,
	type: accounts.type,
	startingBalanceCents: accounts.startingBalanceCents,
	creditLimitCents: accounts.creditLimitCents,
	aprBps: accounts.aprBps,
	monthlyPaymentCents: accounts.monthlyPaymentCents,
	includeInPaydown: accounts.includeInPaydown,
	paymentCategoryId: accounts.paymentCategoryId,
	archivedAt: accounts.archivedAt,
	createdAt: accounts.createdAt,
};

export async function listAccounts(
	userId: string,
	includeArchived: boolean,
): Promise<AccountWithBalance[]> {
	const db = getDb();
	const where = includeArchived
		? eq(accounts.userId, userId)
		: and(eq(accounts.userId, userId), isNull(accounts.archivedAt));
	return db
		.select({ ...accountColumns, balanceCents: balanceExpr })
		.from(accounts)
		.where(where)
		.orderBy(asc(accounts.name));
}

export async function getAccount(
	userId: string,
	id: number,
): Promise<AccountWithBalance | null> {
	const db = getDb();
	const rows = await db
		.select({ ...accountColumns, balanceCents: balanceExpr })
		.from(accounts)
		.where(and(eq(accounts.id, id), eq(accounts.userId, userId)))
		.limit(1);
	return rows[0] ?? null;
}

export async function createAccount(
	userId: string,
	input: CreateAccountInput,
): Promise<number> {
	const db = getDb();
	const [row] = await db
		.insert(accounts)
		.values({
			userId,
			name: input.name,
			type: input.type,
			startingBalanceCents: input.startingBalanceCents,
			creditLimitCents: input.creditLimitCents ?? null,
			aprBps: input.aprBps ?? null,
			monthlyPaymentCents: input.monthlyPaymentCents ?? null,
			includeInPaydown: input.includeInPaydown,
			paymentCategoryId: input.paymentCategoryId ?? null,
		})
		.returning({ id: accounts.id });
	return row.id;
}
