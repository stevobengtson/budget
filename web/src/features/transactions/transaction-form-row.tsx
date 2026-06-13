import { CheckIcon, XIcon } from "lucide-react";
import type * as React from "react";
import { useState } from "react";
import { Button } from "#/components/ui/button.tsx";
import { Input } from "#/components/ui/input.tsx";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "#/components/ui/select.tsx";
import type { Account, Category, Transaction } from "#/lib/api/types.ts";
import type { TransactionInput } from "#/lib/fake/db.ts";
import { formatCents, parseCents } from "#/lib/money.ts";

export function TransactionFormRow({
	transaction,
	accounts,
	categories,
	defaultAccountId,
	defaultDate,
	onSave,
	onCancel,
}: {
	/** undefined = creating new */
	transaction?: Transaction;
	accounts: Account[];
	categories: Category[];
	defaultAccountId: string;
	defaultDate: string;
	onSave: (input: TransactionInput) => void;
	onCancel: () => void;
}) {
	const [date, setDate] = useState(transaction?.date ?? defaultDate);
	const [accountId, setAccountId] = useState(
		transaction?.accountId ?? defaultAccountId,
	);
	const [categoryId, setCategoryId] = useState(transaction?.categoryId ?? "");
	const [payee, setPayee] = useState(transaction?.payee ?? "");
	const [outflow, setOutflow] = useState(
		transaction && transaction.outflowCents > 0
			? formatCents(transaction.outflowCents).replace("$", "")
			: "",
	);
	const [inflow, setInflow] = useState(
		transaction && transaction.inflowCents > 0
			? formatCents(transaction.inflowCents).replace("$", "")
			: "",
	);
	const [error, setError] = useState<string | null>(null);

	function submit() {
		const outflowCents = outflow.trim() ? parseCents(outflow) : 0;
		const inflowCents = inflow.trim() ? parseCents(inflow) : 0;
		if (outflowCents === null || inflowCents === null) {
			setError("Invalid amount");
			return;
		}
		onSave({
			date,
			accountId,
			categoryId: categoryId || null,
			transferAccountId: transaction?.transferAccountId ?? null,
			payee: payee.trim(),
			memo: transaction?.memo ?? "",
			outflowCents,
			inflowCents,
			cleared: transaction?.cleared ?? false,
		});
	}

	function onKeyDown(e: React.KeyboardEvent) {
		if (e.key === "Enter") submit();
		if (e.key === "Escape") onCancel();
	}

	return (
		// biome-ignore lint/a11y/noStaticElementInteractions: keyboard handler delegates Enter/Escape for the inline form inputs
		<div
			className="flex items-center gap-2 border-b bg-accent/40 px-3 py-1.5"
			onKeyDown={onKeyDown}
		>
			<Input
				type="date"
				value={date}
				onChange={(e) => setDate(e.target.value)}
				className="h-8 w-36"
				aria-label="Date"
			/>
			<Select value={accountId} onValueChange={setAccountId}>
				<SelectTrigger className="h-8 w-40" aria-label="Account">
					<SelectValue placeholder="Account" />
				</SelectTrigger>
				<SelectContent>
					{accounts.map((a) => (
						<SelectItem key={a.id} value={a.id}>
							{a.name}
						</SelectItem>
					))}
				</SelectContent>
			</Select>
			<Select value={categoryId} onValueChange={setCategoryId}>
				<SelectTrigger className="h-8 w-44" aria-label="Category">
					<SelectValue placeholder="Category" />
				</SelectTrigger>
				<SelectContent>
					{categories.map((c) => (
						<SelectItem key={c.id} value={c.id}>
							{c.name}
						</SelectItem>
					))}
				</SelectContent>
			</Select>
			<Input
				value={payee}
				onChange={(e) => setPayee(e.target.value)}
				placeholder="Payee"
				className="h-8 flex-1"
				aria-label="Payee"
				autoFocus
			/>
			<Input
				value={outflow}
				onChange={(e) => setOutflow(e.target.value)}
				placeholder="Outflow"
				className="h-8 w-28 text-right"
				aria-label="Outflow"
			/>
			<Input
				value={inflow}
				onChange={(e) => setInflow(e.target.value)}
				placeholder="Inflow"
				className="h-8 w-28 text-right"
				aria-label="Inflow"
			/>
			{error ? <span className="text-xs text-destructive">{error}</span> : null}
			<Button size="icon" className="size-8" aria-label="Save" onClick={submit}>
				<CheckIcon className="size-4" />
			</Button>
			<Button
				size="icon"
				variant="ghost"
				className="size-8"
				aria-label="Cancel"
				onClick={onCancel}
			>
				<XIcon className="size-4" />
			</Button>
		</div>
	);
}
