import { queryOptions } from "@tanstack/react-query";
import {
	createTransaction,
	deleteTransaction,
	listTransactions,
	type TransactionFilter,
	type TransactionInput,
	toggleCleared,
	updateTransaction,
} from "#/lib/fake/db.ts";
import { fakeLatency } from "#/lib/fake/delay.ts";

export const transactionKeys = {
	list: (filter: TransactionFilter) =>
		["transactions", filter.month ?? "all", filter.accountId ?? "all"] as const,
};

export const transactionsQuery = (filter: TransactionFilter) =>
	queryOptions({
		queryKey: transactionKeys.list(filter),
		queryFn: async () => {
			await fakeLatency();
			return listTransactions(filter);
		},
	});

export async function createTransactionFn(input: TransactionInput) {
	await fakeLatency();
	return createTransaction(input);
}

export async function updateTransactionFn(args: {
	id: string;
	patch: Partial<TransactionInput>;
}) {
	await fakeLatency();
	return updateTransaction(args.id, args.patch);
}

export async function deleteTransactionFn(id: string) {
	await fakeLatency();
	deleteTransaction(id);
}

export async function toggleClearedFn(id: string) {
	await fakeLatency();
	return toggleCleared(id);
}
