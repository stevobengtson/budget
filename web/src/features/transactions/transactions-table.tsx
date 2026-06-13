import {
	flexRender,
	getCoreRowModel,
	useReactTable,
} from "@tanstack/react-table";
import { useVirtualizer } from "@tanstack/react-virtual";
import { type ReactNode, useEffect, useMemo, useRef } from "react";
import type { Account, Category, Transaction } from "#/lib/api/types.ts";
import { cn } from "#/lib/utils.ts";
import {
	buildColumns,
	flattenByDate,
	type TransactionTableMeta,
} from "./columns.tsx";

const ROW_HEIGHT = 44;

export function TransactionsTable({
	transactions,
	accounts,
	categories,
	showAccount,
	today,
	focusedIndex,
	editingId,
	renderEditRow,
	meta,
}: {
	transactions: Transaction[];
	accounts: Account[];
	categories: Category[];
	showAccount: boolean;
	/** ISO date for "Today" header bucketing. */
	today: string;
	/** Hotkey row focus (index into tx-only rows); -1 = none. Styling only. */
	focusedIndex: number;
	/** Transaction currently being edited inline; its row renders the form. */
	editingId?: string | null;
	/** Renders the inline edit form for the row matching editingId. */
	renderEditRow?: (tx: Transaction) => ReactNode;
	meta: Pick<TransactionTableMeta, "onEdit" | "onDelete" | "onToggleCleared">;
}) {
	const flatRows = useMemo(
		() => flattenByDate(transactions, today),
		[transactions, today],
	);
	const columns = useMemo(() => buildColumns(showAccount), [showAccount]);
	const tableMeta: TransactionTableMeta = useMemo(
		() => ({
			accountsById: new Map(accounts.map((a) => [a.id, a])),
			categoriesById: new Map(categories.map((c) => [c.id, c])),
			...meta,
		}),
		[accounts, categories, meta],
	);

	const table = useReactTable({
		data: transactions,
		columns,
		getCoreRowModel: getCoreRowModel(),
		meta: tableMeta,
		getRowId: (tx) => tx.id,
	});

	const scrollRef = useRef<HTMLDivElement>(null);
	const virtualizer = useVirtualizer({
		count: flatRows.length,
		getScrollElement: () => scrollRef.current,
		// ROW_HEIGHT is the initial estimate; rows self-measure via measureElement,
		// so the taller inline edit row is handled automatically.
		estimateSize: () => ROW_HEIGHT,
		overscan: 12,
	});

	// map tx id -> table row for cell rendering
	const rowsById = new Map(table.getRowModel().rows.map((r) => [r.id, r]));
	// tx-only index for focus styling
	const txIndexByKey = useMemo(() => {
		const map = new Map<string, number>();
		let i = 0;
		for (const row of flatRows) {
			if (row.kind === "tx") map.set(row.key, i++);
		}
		return map;
	}, [flatRows]);

	// Keep the hotkey-focused row visible.
	useEffect(() => {
		if (focusedIndex < 0) return;
		const flatIndex = flatRows.findIndex(
			(r) => r.kind === "tx" && txIndexByKey.get(r.key) === focusedIndex,
		);
		if (flatIndex >= 0) virtualizer.scrollToIndex(flatIndex, { align: "auto" });
	}, [focusedIndex, flatRows, txIndexByKey, virtualizer]);

	return (
		<div className="overflow-hidden rounded-lg border bg-card">
			<div className="grid grid-cols-[auto] border-b bg-muted/50 px-3 py-2 text-xs font-medium text-muted-foreground">
				<div className="flex">
					{table.getFlatHeaders().map((header) => (
						<div key={header.id} className="flex-1 px-2">
							{flexRender(header.column.columnDef.header, header.getContext())}
						</div>
					))}
				</div>
			</div>
			<div
				ref={scrollRef}
				className="max-h-[calc(100vh-16rem)] overflow-y-auto"
			>
				<div
					className="relative w-full"
					style={{ height: virtualizer.getTotalSize() }}
				>
					{virtualizer.getVirtualItems().map((virtualRow) => {
						const flat = flatRows[virtualRow.index];
						// No fixed height: rows self-measure so the inline edit form can
						// grow taller than a display row.
						const style = {
							position: "absolute" as const,
							top: 0,
							left: 0,
							width: "100%",
							transform: `translateY(${virtualRow.start}px)`,
						};
						if (flat.kind === "header") {
							return (
								<div
									key={flat.key}
									data-index={virtualRow.index}
									ref={virtualizer.measureElement}
									style={style}
									className="flex items-center bg-muted/30 px-4 py-2 text-xs font-semibold text-muted-foreground"
								>
									{flat.label}
								</div>
							);
						}
						if (
							renderEditRow &&
							editingId &&
							flat.transaction.id === editingId
						) {
							return (
								<div
									key={flat.key}
									data-index={virtualRow.index}
									ref={virtualizer.measureElement}
									style={style}
								>
									{renderEditRow(flat.transaction)}
								</div>
							);
						}
						const row = rowsById.get(flat.transaction.id);
						if (!row) return null;
						const isFocused = txIndexByKey.get(flat.key) === focusedIndex;
						return (
							<div
								key={flat.key}
								data-index={virtualRow.index}
								ref={virtualizer.measureElement}
								style={style}
								data-focused={isFocused || undefined}
								className={cn(
									"group flex items-center border-b px-2 py-2.5 text-sm",
									"hover:bg-accent/50 data-[focused]:bg-accent",
								)}
							>
								{row.getVisibleCells().map((cell) => (
									<div key={cell.id} className="flex-1 px-2">
										{flexRender(cell.column.columnDef.cell, cell.getContext())}
									</div>
								))}
							</div>
						);
					})}
				</div>
			</div>
		</div>
	);
}
