import { queryOptions } from "@tanstack/react-query";
import {
	accountMonthSummary,
	type CreateAccountInput,
	createAccount,
	getAccount,
	listAccounts,
} from "#/lib/fake/db.ts";
import { fakeLatency } from "#/lib/fake/delay.ts";

export const accountKeys = {
	all: ["accounts"] as const,
	detail: (id: string) => ["accounts", id] as const,
	summary: (id: string, month: string) =>
		["accounts", id, "summary", month] as const,
};

export const accountsQuery = () =>
	queryOptions({
		queryKey: accountKeys.all,
		queryFn: async () => {
			await fakeLatency();
			return listAccounts();
		},
	});

export const accountQuery = (id: string) =>
	queryOptions({
		queryKey: accountKeys.detail(id),
		queryFn: async () => {
			await fakeLatency();
			return getAccount(id);
		},
	});

export const accountSummaryQuery = (id: string, month: string) =>
	queryOptions({
		queryKey: accountKeys.summary(id, month),
		queryFn: async () => {
			await fakeLatency();
			return accountMonthSummary(id, month);
		},
	});

export async function createAccountFn(input: CreateAccountInput) {
	await fakeLatency();
	return createAccount(input);
}
