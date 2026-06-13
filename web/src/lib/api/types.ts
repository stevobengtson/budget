export type AccountType = "checking" | "savings" | "credit" | "loan";

export interface Account {
	id: string;
	name: string;
	type: AccountType;
	/** Current balance. Negative for debt (credit/loan). */
	balanceCents: number;
	/** Credit limit (credit) or original principal (loan). */
	limitCents: number | null;
	/** APR in basis points (e.g. 2199 = 21.99%). */
	aprBps: number | null;
}

export interface Transaction {
	id: string;
	accountId: string;
	/** ISO date YYYY-MM-DD */
	date: string;
	payee: string;
	/** null when this is a transfer or uncategorized */
	categoryId: string | null;
	/** Set when the row is a transfer to/from another account. */
	transferAccountId: string | null;
	memo: string;
	outflowCents: number;
	inflowCents: number;
	cleared: boolean;
}

export interface CategoryGroup {
	id: string;
	name: string;
	sortOrder: number;
}

export interface Category {
	id: string;
	groupId: string;
	name: string;
	/** Monthly goal; null = no goal. */
	goalCents: number | null;
	/** System categories (Income) cannot be edited/deleted. */
	locked: boolean;
}

export interface IncomeSource {
	id: string;
	name: string;
	amountCents: number;
	/** Day of month the income arrives (1–31). */
	dayOfMonth: number;
}

/** Per-month computed view of one category. */
export interface BudgetCategoryRow {
	categoryId: string;
	name: string;
	goalCents: number | null;
	assignedCents: number;
	/** Spending is negative, refunds positive. */
	activityCents: number;
	/** assigned + carryover + activity (cumulative). */
	availableCents: number;
}

export interface BudgetGroupRow {
	groupId: string;
	name: string;
	sortOrder: number;
	categories: BudgetCategoryRow[];
	assignedCents: number;
	activityCents: number;
	availableCents: number;
}

export interface BudgetMonth {
	/** YYYY-MM */
	month: string;
	incomeCents: number;
	assignedCents: number;
	/** income - assigned */
	remainingCents: number;
	activityCents: number;
	availableCents: number;
	groups: BudgetGroupRow[];
}

export interface AccountMonthSummary {
	accountId: string;
	month: string;
	balanceCents: number;
	inflowCents: number;
	outflowCents: number;
	netCents: number;
}
