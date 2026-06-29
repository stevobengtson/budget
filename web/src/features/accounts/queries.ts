import { queryOptions } from "@tanstack/react-query";
// TODO(plan-2 budget domain): accountMonthSummary is still fake until month
// aggregation is ported. Remove this import when summaries move to the API.
import { accountMonthSummary } from "#/lib/fake/db.ts";
import { fakeLatency } from "#/lib/fake/delay.ts";
import {
	createAccountFn,
	fetchAccount,
	fetchAccounts,
} from "@/server/accounts";
import type { CreateAccountInput } from "@/server/db/accounts";

export const accountKeys = {
	all: ["accounts"] as const,
	detail: (id: number) => ["accounts", id] as const,
	summary: (id: number, month: string) =>
		["accounts", id, "summary", month] as const,
};

export const accountsQuery = () =>
	queryOptions({
		queryKey: accountKeys.all,
		queryFn: () => fetchAccounts(),
	});

export const accountQuery = (id: number) =>
	queryOptions({
		queryKey: accountKeys.detail(id),
		queryFn: () => fetchAccount({ data: id }),
	});

export const accountSummaryQuery = (id: number, month: string) =>
	queryOptions({
		queryKey: accountKeys.summary(id, month),
		queryFn: async () => {
			await fakeLatency();
			return accountMonthSummary(String(id), month);
		},
	});

export async function createAccountMutation(input: CreateAccountInput) {
	return createAccountFn({ data: input });
}
