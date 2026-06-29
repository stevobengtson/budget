import {
	useMutation,
	useQueryClient,
	useSuspenseQuery,
} from "@tanstack/react-query";
import { PlusIcon } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { ConfirmDialog } from "#/components/confirm-dialog.tsx";
import { Button } from "#/components/ui/button.tsx";
import { accountsQuery } from "#/features/accounts/queries.ts";
import { categoriesQuery } from "#/features/budget/queries.ts";
import type { Account, Transaction } from "#/lib/api/types.ts";
import { useAppHotkeys } from "#/lib/hotkeys.ts";
import { FilterBar } from "./filter-bar.tsx";
import {
	createTransactionFn,
	deleteTransactionFn,
	toggleClearedFn,
	transactionsQuery,
	updateTransactionFn,
} from "./queries.ts";
import { TransactionFormRow } from "./transaction-form-row.tsx";
import { TransactionsTable } from "./transactions-table.tsx";

export function TransactionsScreen({
	month,
	accountId,
	filter,
	onFilterChange,
}: {
	month: string;
	accountId?: string;
	filter: string;
	onFilterChange: (value: string) => void;
}) {
	const queryClient = useQueryClient();
	const { data: transactions } = useSuspenseQuery(
		transactionsQuery({ month, accountId }),
	);
	const { data: accountsResponse } = useSuspenseQuery(accountsQuery());
	// Convert AccountWithBalance[] to the legacy Account shape that transaction
	// sub-components expect. Transactions haven't migrated off the fake layer yet;
	// string-coercing ids here keeps TypeScript happy until that cutover happens.
	const accounts: Account[] = accountsResponse.accounts.map((a) => ({
		id: String(a.id),
		name: a.name,
		type: a.type,
		balanceCents: a.balanceCents,
		limitCents: a.creditLimitCents,
		aprBps: a.aprBps,
	}));
	const { data: categories } = useSuspenseQuery(categoriesQuery());
	const [deleting, setDeleting] = useState<Transaction | null>(null);
	const [editing, setEditing] = useState<Transaction | null>(null);
	const [adding, setAdding] = useState(false);
	const [focusedIndex, setFocusedIndex] = useState(-1);

	const invalidate = () =>
		queryClient
			.invalidateQueries({ queryKey: ["transactions"] })
			.then(() => queryClient.invalidateQueries({ queryKey: ["accounts"] }));

	const deleteMutation = useMutation({
		mutationFn: deleteTransactionFn,
		onSuccess: invalidate,
		onError: (error) => toast.error(error.message),
	});
	const clearedMutation = useMutation({
		mutationFn: toggleClearedFn,
		onSuccess: invalidate,
		onError: (error) => toast.error(error.message),
	});
	const createMutation = useMutation({
		mutationFn: createTransactionFn,
		onSuccess: () => {
			setAdding(false);
			return invalidate();
		},
		onError: (error) => toast.error(error.message),
	});
	const updateMutation = useMutation({
		mutationFn: updateTransactionFn,
		onSuccess: () => {
			setEditing(null);
			return invalidate();
		},
		onError: (error) => toast.error(error.message),
	});

	const today = new Date().toISOString().slice(0, 10);
	const defaultAccountId = accountId ?? accounts[0]?.id ?? "";

	const visible = useMemo(() => {
		const q = filter.trim().toLowerCase();
		if (!q) return transactions;
		const categoryName = (tx: Transaction) =>
			categories.find((c) => c.id === tx.categoryId)?.name ?? "";
		return transactions.filter((tx) =>
			[tx.payee, tx.memo, categoryName(tx)].some((s) =>
				s.toLowerCase().includes(q),
			),
		);
	}, [transactions, categories, filter]);

	// Table-scope keys. visible's order matches the table's tx-row render order.
	useAppHotkeys([
		{
			key: "j",
			handler: () =>
				setFocusedIndex((i) => Math.min(i + 1, visible.length - 1)),
			help: { label: "Next row", group: "Table" },
		},
		{
			key: "k",
			handler: () => setFocusedIndex((i) => Math.max(i - 1, 0)),
			help: { label: "Previous row", group: "Table" },
		},
		{
			key: "arrowdown",
			handler: () =>
				setFocusedIndex((i) => Math.min(i + 1, visible.length - 1)),
		},
		{
			key: "arrowup",
			handler: () => setFocusedIndex((i) => Math.max(i - 1, 0)),
		},
		{
			key: "enter",
			handler: () => focusedIndex >= 0 && setEditing(visible[focusedIndex]),
			help: { label: "Edit focused row", group: "Table" },
		},
		{
			key: "d",
			handler: () => focusedIndex >= 0 && setDeleting(visible[focusedIndex]),
			help: { label: "Delete focused row", group: "Table" },
		},
		{
			key: "c",
			handler: () =>
				focusedIndex >= 0 && clearedMutation.mutate(visible[focusedIndex].id),
			help: { label: "Toggle cleared", group: "Table" },
		},
		{
			key: "n",
			handler: () => setAdding(true),
			help: { label: "New transaction", group: "Table" },
		},
	]);

	return (
		<div className="flex flex-col gap-3">
			<div className="flex items-center justify-between gap-2">
				<FilterBar initialValue={filter} onFilterChange={onFilterChange} />
				<Button onClick={() => setAdding(true)} disabled={adding}>
					<PlusIcon className="size-4" />
					New Transaction
				</Button>
			</div>
			{adding ? (
				<div className="overflow-hidden rounded-lg border bg-card">
					<TransactionFormRow
						accounts={accounts}
						categories={categories}
						defaultAccountId={defaultAccountId}
						defaultDate={today}
						onSave={(input) => createMutation.mutate(input)}
						onCancel={() => setAdding(false)}
					/>
				</div>
			) : null}
			<TransactionsTable
				transactions={visible}
				accounts={accounts}
				categories={categories}
				showAccount={!accountId}
				today={today}
				focusedIndex={focusedIndex}
				editingId={editing?.id ?? null}
				renderEditRow={(tx) => (
					<TransactionFormRow
						transaction={tx}
						accounts={accounts}
						categories={categories}
						defaultAccountId={defaultAccountId}
						defaultDate={tx.date}
						onSave={(input) =>
							updateMutation.mutate({ id: tx.id, patch: input })
						}
						onCancel={() => setEditing(null)}
					/>
				)}
				meta={{
					onEdit: setEditing,
					onDelete: setDeleting,
					onToggleCleared: (tx) => clearedMutation.mutate(tx.id),
				}}
			/>
			<ConfirmDialog
				open={deleting !== null}
				onOpenChange={(open) => !open && setDeleting(null)}
				title="Delete transaction?"
				description={`"${deleting?.payee ?? ""}" will be removed and the account balance adjusted.`}
				onConfirm={() => {
					if (deleting) deleteMutation.mutate(deleting.id);
					setDeleting(null);
				}}
			/>
		</div>
	);
}
