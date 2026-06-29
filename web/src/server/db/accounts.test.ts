import { sql } from "drizzle-orm";
import { afterAll, beforeEach, describe, expect, it } from "vitest";
import { accounts, transactions } from "../../db/schema/budget-schema.ts";
import { getDb } from "../../lib/db.ts";
import { createAccount, getAccount, listAccounts } from "./accounts.ts";

const USER = "00000000-0000-0000-0000-0000000000aa";

async function reset() {
	const db = getDb();
	await db.execute(
		sql`TRUNCATE transactions, accounts RESTART IDENTITY CASCADE`,
	);
}

beforeEach(reset);
afterAll(reset);

describe("account queries", () => {
	it("creates and reads back an account", async () => {
		const id = await createAccount(USER, {
			name: "Checking",
			type: "checking",
			startingBalanceCents: 10_000,
			includeInPaydown: false,
		});
		const acct = await getAccount(USER, id);
		expect(acct?.name).toBe("Checking");
		expect(acct?.balanceCents).toBe(10_000);
	});

	it("computes balance from transactions", async () => {
		const db = getDb();
		const id = await createAccount(USER, {
			name: "Wallet",
			type: "cash",
			startingBalanceCents: 5_000,
			includeInPaydown: false,
		});
		await db.insert(transactions).values([
			{
				userId: USER,
				date: "2026-01-01",
				accountId: id,
				inflowCents: 2_000,
				outflowCents: 0,
			},
			{
				userId: USER,
				date: "2026-01-02",
				accountId: id,
				inflowCents: 0,
				outflowCents: 1_500,
			},
		]);
		const list = await listAccounts(USER, false);
		expect(list).toHaveLength(1);
		expect(list[0].balanceCents).toBe(5_500);
	});

	it("does not return another user's accounts", async () => {
		await createAccount("00000000-0000-0000-0000-0000000000bb", {
			name: "Theirs",
			type: "checking",
			startingBalanceCents: 0,
			includeInPaydown: false,
		});
		const list = await listAccounts(USER, false);
		expect(list).toHaveLength(0);
	});

	it("excludes archived accounts unless asked", async () => {
		const db = getDb();
		const id = await createAccount(USER, {
			name: "Old",
			type: "savings",
			startingBalanceCents: 0,
			includeInPaydown: false,
		});
		await db
			.update(accounts)
			.set({ archivedAt: new Date() })
			.where(sql`${accounts.id} = ${id}`);
		expect(await listAccounts(USER, false)).toHaveLength(0);
		expect(await listAccounts(USER, true)).toHaveLength(1);
	});

	it("getAccount does not return another user's account", async () => {
		const otherId = await createAccount(
			"00000000-0000-0000-0000-0000000000bb",
			{
				name: "Theirs",
				type: "checking",
				startingBalanceCents: 0,
				includeInPaydown: false,
			},
		);
		expect(await getAccount(USER, otherId)).toBeNull();
	});

	it("getAccount returns null for an unknown id", async () => {
		expect(await getAccount(USER, 999999)).toBeNull();
	});

	it("includes active accounts alongside archived when includeArchived is true", async () => {
		const db = getDb();
		await createAccount(USER, {
			name: "Active",
			type: "checking",
			startingBalanceCents: 0,
			includeInPaydown: false,
		});
		const archivedId = await createAccount(USER, {
			name: "Archived",
			type: "savings",
			startingBalanceCents: 0,
			includeInPaydown: false,
		});
		await db
			.update(accounts)
			.set({ archivedAt: new Date() })
			.where(sql`${accounts.id} = ${archivedId}`);
		expect(await listAccounts(USER, false)).toHaveLength(1);
		expect(await listAccounts(USER, true)).toHaveLength(2);
	});
});
