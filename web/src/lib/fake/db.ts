import type {
	Account,
	AccountMonthSummary,
	BudgetCategoryRow,
	BudgetGroupRow,
	BudgetMonth,
	Category,
	CategoryGroup,
	IncomeSource,
	Transaction,
} from "../api/types.ts";
import { addMonths, isInMonth, todayMonth } from "../month.ts";
import {
	accountSeeds,
	buildAssignmentSeeds,
	buildTransactionSeeds,
	categorySeeds,
	groupSeeds,
	incomeSeeds,
} from "./fixtures.ts";

const INCOME_GROUP: CategoryGroup = {
	id: "grp-income",
	name: "Income",
	sortOrder: 0,
};
const INCOME_CATEGORY: Category = {
	id: "cat-income",
	groupId: "grp-income",
	name: "Income",
	goalCents: null,
	locked: true,
};

interface Store {
	accounts: (Account & { openingBalanceCents: number })[];
	groups: CategoryGroup[];
	categories: Category[];
	transactions: Transaction[];
	incomeSources: IncomeSource[];
	/** month -> categoryId -> assignedCents */
	assignments: Map<string, Map<string, number>>;
	idCounter: number;
}

let store: Store = seed();

function seed(): Store {
	return {
		accounts: accountSeeds.map((a) => ({ ...a })),
		groups: [INCOME_GROUP, ...groupSeeds.map((g) => ({ ...g }))],
		categories: [INCOME_CATEGORY, ...categorySeeds.map((c) => ({ ...c }))],
		transactions: buildTransactionSeeds(),
		incomeSources: incomeSeeds.map((i) => ({ ...i })),
		assignments: buildAssignmentSeeds(),
		idCounter: 10_000,
	};
}

export function resetDb(): void {
	store = seed();
}

function nextId(prefix: string): string {
	return `${prefix}-${++store.idCounter}`;
}

// ---------- accounts ----------

function balanceOf(accountId: string): number {
	const acc = store.accounts.find((a) => a.id === accountId);
	if (!acc) throw new Error(`Unknown account: ${accountId}`);
	return store.transactions
		.filter((t) => t.accountId === accountId)
		.reduce(
			(sum, t) => sum + t.inflowCents - t.outflowCents,
			acc.openingBalanceCents,
		);
}

export function listAccounts(): Account[] {
	return store.accounts.map(({ openingBalanceCents: _o, ...a }) => ({
		...a,
		balanceCents: balanceOf(a.id),
	}));
}

export function getAccount(id: string): Account {
	const account = listAccounts().find((a) => a.id === id);
	if (!account) throw new Error(`Unknown account: ${id}`);
	return account;
}

export interface CreateAccountInput {
	name: string;
	type: Account["type"];
	balanceCents: number;
	limitCents: number | null;
	aprBps: number | null;
}

export function createAccount(input: CreateAccountInput): Account {
	if (!input.name.trim()) throw new Error("Account name is required");
	const account = {
		id: nextId("acc"),
		name: input.name.trim(),
		type: input.type,
		balanceCents: input.balanceCents,
		openingBalanceCents: input.balanceCents,
		limitCents: input.limitCents,
		aprBps: input.aprBps,
	};
	store.accounts.push(account);
	return getAccount(account.id);
}

export function accountMonthSummary(
	accountId: string,
	month: string,
): AccountMonthSummary {
	const txs = store.transactions.filter(
		(t) => t.accountId === accountId && isInMonth(t.date, month),
	);
	const inflowCents = txs.reduce((s, t) => s + t.inflowCents, 0);
	const outflowCents = txs.reduce((s, t) => s + t.outflowCents, 0);
	return {
		accountId,
		month,
		balanceCents: balanceOf(accountId),
		inflowCents,
		outflowCents,
		netCents: inflowCents - outflowCents,
	};
}

// ---------- transactions ----------

export interface TransactionFilter {
	month?: string;
	accountId?: string;
}

export function listTransactions(filter: TransactionFilter): Transaction[] {
	return store.transactions
		.filter((t) => (filter.month ? isInMonth(t.date, filter.month) : true))
		.filter((t) => (filter.accountId ? t.accountId === filter.accountId : true))
		.sort((a, b) =>
			a.date < b.date ? 1 : a.date > b.date ? -1 : a.id < b.id ? 1 : -1,
		)
		.map((t) => ({ ...t }));
}

export type TransactionInput = Omit<Transaction, "id">;

function validateTransaction(input: TransactionInput): void {
	if (!input.payee.trim()) throw new Error("Payee is required");
	if (input.outflowCents < 0 || input.inflowCents < 0)
		throw new Error("Amounts must be positive");
	if (input.outflowCents === 0 && input.inflowCents === 0)
		throw new Error("Enter an outflow or inflow amount");
	if (!/^\d{4}-\d{2}-\d{2}$/.test(input.date)) throw new Error("Invalid date");
}

export function createTransaction(input: TransactionInput): Transaction {
	validateTransaction(input);
	const tx: Transaction = { ...input, id: nextId("tx") };
	store.transactions.push(tx);
	return { ...tx };
}

export function updateTransaction(
	id: string,
	patch: Partial<TransactionInput>,
): Transaction {
	const tx = store.transactions.find((t) => t.id === id);
	if (!tx) throw new Error(`Unknown transaction: ${id}`);
	const next = { ...tx, ...patch };
	validateTransaction(next);
	Object.assign(tx, next);
	return { ...tx };
}

export function deleteTransaction(id: string): void {
	const index = store.transactions.findIndex((t) => t.id === id);
	if (index === -1) throw new Error(`Unknown transaction: ${id}`);
	store.transactions.splice(index, 1);
}

export function toggleCleared(id: string): Transaction {
	const tx = store.transactions.find((t) => t.id === id);
	if (!tx) throw new Error(`Unknown transaction: ${id}`);
	tx.cleared = !tx.cleared;
	return { ...tx };
}

// ---------- budget ----------

const FIRST_MONTH = addMonths(todayMonth(), -2);

function assignedFor(month: string, categoryId: string): number {
	return store.assignments.get(month)?.get(categoryId) ?? 0;
}

function activityFor(month: string, categoryId: string): number {
	return store.transactions
		.filter((t) => t.categoryId === categoryId && isInMonth(t.date, month))
		.reduce((s, t) => s + t.inflowCents - t.outflowCents, 0);
}

/** Cumulative assigned + activity from FIRST_MONTH through `month`. */
function availableFor(month: string, categoryId: string): number {
	let total = 0;
	for (let m = FIRST_MONTH; m <= month; m = addMonths(m, 1)) {
		total += assignedFor(m, categoryId) + activityFor(m, categoryId);
	}
	return total;
}

export function getBudgetMonth(month: string): BudgetMonth {
	const incomeCents = store.transactions
		.filter(
			(t) => t.categoryId === INCOME_CATEGORY.id && isInMonth(t.date, month),
		)
		.reduce((s, t) => s + t.inflowCents - t.outflowCents, 0);

	const groups: BudgetGroupRow[] = store.groups
		.filter((g) => g.id !== INCOME_GROUP.id)
		.sort((a, b) => a.sortOrder - b.sortOrder)
		.map((g) => {
			const categories: BudgetCategoryRow[] = store.categories
				.filter((c) => c.groupId === g.id)
				.map((c) => ({
					categoryId: c.id,
					name: c.name,
					goalCents: c.goalCents,
					assignedCents: assignedFor(month, c.id),
					activityCents: activityFor(month, c.id),
					availableCents: availableFor(month, c.id),
				}));
			return {
				groupId: g.id,
				name: g.name,
				sortOrder: g.sortOrder,
				categories,
				assignedCents: categories.reduce((s, c) => s + c.assignedCents, 0),
				activityCents: categories.reduce((s, c) => s + c.activityCents, 0),
				availableCents: categories.reduce((s, c) => s + c.availableCents, 0),
			};
		});

	const assignedCents = groups.reduce((s, g) => s + g.assignedCents, 0);
	const activityCents = groups.reduce((s, g) => s + g.activityCents, 0);
	return {
		month,
		incomeCents,
		assignedCents,
		remainingCents: incomeCents - assignedCents,
		activityCents,
		availableCents: groups.reduce((s, g) => s + g.availableCents, 0),
		groups,
	};
}

export function assignBudget(
	month: string,
	categoryId: string,
	cents: number,
): void {
	if (cents < 0) throw new Error("Assigned amount must be positive");
	let monthMap = store.assignments.get(month);
	if (!monthMap) {
		monthMap = new Map();
		store.assignments.set(month, monthMap);
	}
	monthMap.set(categoryId, cents);
}

// ---------- groups & categories ----------

export function listGroups(): CategoryGroup[] {
	return store.groups
		.filter((g) => g.id !== INCOME_GROUP.id)
		.map((g) => ({ ...g }));
}

export function listCategories(): Category[] {
	return store.categories.filter((c) => !c.locked).map((c) => ({ ...c }));
}

export function createGroup(name: string): CategoryGroup {
	if (!name.trim()) throw new Error("Group name is required");
	const group = {
		id: nextId("grp"),
		name: name.trim(),
		sortOrder: Math.max(...store.groups.map((g) => g.sortOrder)) + 1,
	};
	store.groups.push(group);
	return { ...group };
}

export function updateGroup(id: string, name: string): CategoryGroup {
	const group = store.groups.find((g) => g.id === id);
	if (!group || group.id === INCOME_GROUP.id)
		throw new Error(`Unknown group: ${id}`);
	if (!name.trim()) throw new Error("Group name is required");
	group.name = name.trim();
	return { ...group };
}

export function deleteGroup(id: string): void {
	if (store.categories.some((c) => c.groupId === id))
		throw new Error("Move or delete this group's categories first");
	const index = store.groups.findIndex((g) => g.id === id);
	if (index === -1) throw new Error(`Unknown group: ${id}`);
	store.groups.splice(index, 1);
}

export interface CategoryInput {
	groupId: string;
	name: string;
	goalCents: number | null;
}

export function createCategory(input: CategoryInput): Category {
	if (!input.name.trim()) throw new Error("Category name is required");
	if (!store.groups.some((g) => g.id === input.groupId))
		throw new Error(`Unknown group: ${input.groupId}`);
	const category: Category = {
		id: nextId("cat"),
		groupId: input.groupId,
		name: input.name.trim(),
		goalCents: input.goalCents,
		locked: false,
	};
	store.categories.push(category);
	return { ...category };
}

export function updateCategory(
	id: string,
	patch: Partial<CategoryInput>,
): Category {
	const category = store.categories.find((c) => c.id === id);
	if (!category || category.locked) throw new Error(`Unknown category: ${id}`);
	if (patch.name !== undefined && !patch.name.trim())
		throw new Error("Category name is required");
	Object.assign(category, {
		...patch,
		...(patch.name !== undefined ? { name: patch.name.trim() } : {}),
	});
	return { ...category };
}

export function deleteCategory(id: string): void {
	const category = store.categories.find((c) => c.id === id);
	if (!category || category.locked) throw new Error(`Unknown category: ${id}`);
	// detach transactions instead of orphaning them
	for (const tx of store.transactions) {
		if (tx.categoryId === id) tx.categoryId = null;
	}
	store.categories.splice(store.categories.indexOf(category), 1);
}

// ---------- income ----------

export function listIncomeSources(): IncomeSource[] {
	return store.incomeSources.map((i) => ({ ...i }));
}

export function upsertIncomeSource(
	input: Omit<IncomeSource, "id"> & { id?: string },
): IncomeSource {
	if (!input.name.trim()) throw new Error("Name is required");
	if (input.amountCents <= 0) throw new Error("Amount must be positive");
	if (input.dayOfMonth < 1 || input.dayOfMonth > 31)
		throw new Error("Day must be 1–31");
	if (input.id) {
		const existing = store.incomeSources.find((i) => i.id === input.id);
		if (!existing) throw new Error(`Unknown income source: ${input.id}`);
		Object.assign(existing, input);
		return { ...existing };
	}
	const created: IncomeSource = { ...input, id: nextId("inc") };
	store.incomeSources.push(created);
	return { ...created };
}

export function deleteIncomeSource(id: string): void {
	const index = store.incomeSources.findIndex((i) => i.id === id);
	if (index === -1) throw new Error(`Unknown income source: ${id}`);
	store.incomeSources.splice(index, 1);
}
