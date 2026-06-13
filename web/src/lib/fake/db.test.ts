import { beforeEach, describe, expect, it } from "vitest";
import { todayMonth } from "../month.ts";
import {
	assignBudget,
	createCategory,
	createTransaction,
	deleteTransaction,
	getAccount,
	getBudgetMonth,
	listAccounts,
	listTransactions,
	resetDb,
	toggleCleared,
} from "./db.ts";

beforeEach(() => resetDb());

describe("accounts", () => {
	it("derives balances from opening balance + transactions", () => {
		const checking = getAccount("acc-checking");
		const manual =
			412500 +
			listTransactions({ accountId: "acc-checking" }).reduce(
				(sum, t) => sum + t.inflowCents - t.outflowCents,
				0,
			);
		expect(checking.balanceCents).toBe(manual);
	});
	it("lists 4 accounts", () => {
		expect(listAccounts()).toHaveLength(4);
	});
});

describe("transactions", () => {
	it("filters by month and account, sorted newest first", () => {
		const month = todayMonth();
		const txs = listTransactions({ month, accountId: "acc-credit" });
		expect(txs.length).toBeGreaterThan(0);
		expect(txs.every((t) => t.accountId === "acc-credit")).toBe(true);
		expect(txs.every((t) => t.date.startsWith(month))).toBe(true);
		const dates = txs.map((t) => t.date);
		expect(dates).toEqual([...dates].sort().reverse());
	});
	it("create/delete adjusts the account balance", () => {
		const before = getAccount("acc-checking").balanceCents;
		const tx = createTransaction({
			accountId: "acc-checking",
			date: `${todayMonth()}-05`,
			payee: "Test Payee",
			categoryId: "cat-groceries",
			transferAccountId: null,
			memo: "",
			outflowCents: 1000,
			inflowCents: 0,
			cleared: false,
		});
		expect(getAccount("acc-checking").balanceCents).toBe(before - 1000);
		deleteTransaction(tx.id);
		expect(getAccount("acc-checking").balanceCents).toBe(before);
	});
	it("toggleCleared flips the flag", () => {
		const tx = listTransactions({})[0];
		const was = tx.cleared;
		toggleCleared(tx.id);
		expect(listTransactions({}).find((t) => t.id === tx.id)?.cleared).toBe(
			!was,
		);
	});
});

describe("budget", () => {
	it("computes activity from the month's categorized spending", () => {
		const month = todayMonth();
		const budget = getBudgetMonth(month);
		const food = budget.groups.find((g) => g.name === "Food");
		expect(food).toBeDefined();
		const groceries = food?.categories.find((c) => c.name === "Groceries");
		const manual = listTransactions({ month }).filter(
			(t) => t.categoryId === "cat-groceries",
		);
		const expected = manual.reduce(
			(s, t) => s + t.inflowCents - t.outflowCents,
			0,
		);
		expect(groceries?.activityCents).toBe(expected);
	});
	it("assignBudget updates assigned + remaining", () => {
		const month = todayMonth();
		const before = getBudgetMonth(month);
		assignBudget(month, "cat-groceries", 60000);
		const after = getBudgetMonth(month);
		const cat = after.groups
			.flatMap((g) => g.categories)
			.find((c) => c.categoryId === "cat-groceries");
		expect(cat?.assignedCents).toBe(60000);
		expect(after.remainingCents).toBe(before.remainingCents - (60000 - 50000));
	});
	it("available carries over from prior months", () => {
		const month = todayMonth();
		const cat = getBudgetMonth(month)
			.groups.flatMap((g) => g.categories)
			.find((c) => c.categoryId === "cat-parking");
		// 3 months of assignments + activity accumulate (never reset)
		expect(cat?.availableCents).toBeDefined();
		const oneMonth = 4000 - 1800;
		expect(cat?.availableCents).toBeGreaterThanOrEqual(oneMonth);
	});
	it("excludes the system Income group from groups", () => {
		const budget = getBudgetMonth(todayMonth());
		expect(budget.groups.some((g) => g.name === "Income")).toBe(false);
	});
	it("createCategory appears in its group with zero values", () => {
		createCategory({ groupId: "grp-misc", name: "Hobbies", goalCents: 3000 });
		const misc = getBudgetMonth(todayMonth()).groups.find(
			(g) => g.name === "Misc",
		);
		const row = misc?.categories.find((c) => c.name === "Hobbies");
		expect(row).toMatchObject({ assignedCents: 0, activityCents: 0 });
	});
});
