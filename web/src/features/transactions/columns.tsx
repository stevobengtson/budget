import type { ColumnDef } from "@tanstack/react-table";
import { CheckIcon, PencilIcon, Trash2Icon } from "lucide-react";
import { Amount } from "#/components/amount.tsx";
import { Button } from "#/components/ui/button.tsx";
import type { Account, Category, Transaction } from "#/lib/api/types.ts";
import { cn } from "#/lib/utils.ts";

export interface TransactionTableMeta {
	accountsById: Map<string, Account>;
	categoriesById: Map<string, Category>;
	onEdit: (tx: Transaction) => void;
	onDelete: (tx: Transaction) => void;
	onToggleCleared: (tx: Transaction) => void;
}

function formatDay(isoDate: string): string {
	const [y, m, d] = isoDate.split("-").map(Number);
	return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString("en-US", {
		month: "short",
		day: "numeric",
		year: "numeric",
		timeZone: "UTC",
	});
}

export function buildColumns(showAccount: boolean): ColumnDef<Transaction>[] {
	const columns: ColumnDef<Transaction>[] = [
		{
			id: "date",
			header: "Date",
			accessorKey: "date",
			cell: ({ getValue }) => (
				<span className="text-muted-foreground tabular-nums">
					{formatDay(getValue<string>())}
				</span>
			),
		},
	];

	if (showAccount) {
		columns.push({
			id: "account",
			header: "Account",
			cell: ({ row, table }) => {
				const meta = table.options.meta as TransactionTableMeta;
				return meta.accountsById.get(row.original.accountId)?.name ?? "—";
			},
		});
	}

	columns.push(
		{
			id: "category",
			header: "Category / Transfer",
			cell: ({ row, table }) => {
				const meta = table.options.meta as TransactionTableMeta;
				const tx = row.original;
				if (tx.transferAccountId) {
					const name = meta.accountsById.get(tx.transferAccountId)?.name ?? "?";
					return (
						<span className="text-muted-foreground italic">
							Transfer : {name}
						</span>
					);
				}
				const category = meta.categoriesById.get(tx.categoryId ?? "");
				if (category) return category.name;
				// Income transactions reference the locked system category, which is
				// excluded from listCategories(); label them instead of "Uncategorized".
				return (
					<span className="text-muted-foreground">
						{tx.inflowCents > 0 ? "Income" : "Uncategorized"}
					</span>
				);
			},
		},
		{
			id: "payee",
			header: "Payee",
			accessorKey: "payee",
		},
		{
			id: "outflow",
			header: () => <div className="text-right">Outflow</div>,
			cell: ({ row }) =>
				row.original.outflowCents > 0 ? (
					<div className="text-right">
						<Amount cents={-row.original.outflowCents} tone="neutral" />
					</div>
				) : null,
		},
		{
			id: "inflow",
			header: () => <div className="text-right">Inflow</div>,
			cell: ({ row }) =>
				row.original.inflowCents > 0 ? (
					<div className="text-right">
						<Amount cents={row.original.inflowCents} />
					</div>
				) : null,
		},
		{
			id: "cleared",
			header: "",
			cell: ({ row, table }) => {
				const meta = table.options.meta as TransactionTableMeta;
				return (
					<button
						type="button"
						aria-label={row.original.cleared ? "Cleared" : "Uncleared"}
						onClick={() => meta.onToggleCleared(row.original)}
						className={cn(
							"flex size-5 items-center justify-center rounded-full border",
							row.original.cleared
								? "border-emerald-600 bg-emerald-600 text-white dark:border-emerald-400 dark:bg-emerald-500"
								: "border-border text-transparent hover:border-muted-foreground",
						)}
					>
						<CheckIcon className="size-3" />
					</button>
				);
			},
		},
		{
			id: "actions",
			header: "",
			cell: ({ row, table }) => {
				const meta = table.options.meta as TransactionTableMeta;
				return (
					<div className="flex justify-end gap-1 opacity-0 group-hover:opacity-100">
						<Button
							variant="ghost"
							size="icon"
							className="size-7"
							aria-label="Edit transaction"
							onClick={() => meta.onEdit(row.original)}
						>
							<PencilIcon className="size-3.5" />
						</Button>
						<Button
							variant="ghost"
							size="icon"
							className="size-7 text-destructive"
							aria-label="Delete transaction"
							onClick={() => meta.onDelete(row.original)}
						>
							<Trash2Icon className="size-3.5" />
						</Button>
					</div>
				);
			},
		},
	);

	return columns;
}

export type FlatRow =
	| { kind: "header"; key: string; label: string }
	| { kind: "tx"; key: string; transaction: Transaction };

export function flattenByDate(
	transactions: Transaction[],
	today: string,
): FlatRow[] {
	const rows: FlatRow[] = [];
	let lastDate: string | null = null;
	for (const tx of transactions) {
		if (tx.date !== lastDate) {
			lastDate = tx.date;
			rows.push({
				kind: "header",
				key: `header-${tx.date}`,
				label: tx.date === today ? "Today" : formatDay(tx.date),
			});
		}
		rows.push({ kind: "tx", key: tx.id, transaction: tx });
	}
	return rows;
}
