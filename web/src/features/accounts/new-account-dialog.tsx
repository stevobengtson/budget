import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { EntityDialog } from "#/components/entity-dialog.tsx";
import { Input } from "#/components/ui/input.tsx";
import { Label } from "#/components/ui/label.tsx";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "#/components/ui/select.tsx";
import type { AccountType } from "#/lib/api/types.ts";
import { parseCents } from "#/lib/money.ts";
import { accountKeys, createAccountFn } from "./queries.ts";

const TYPES: { value: AccountType; label: string }[] = [
	{ value: "checking", label: "Checking" },
	{ value: "savings", label: "Savings" },
	{ value: "credit", label: "Credit Card" },
	{ value: "loan", label: "Loan" },
];

export function NewAccountDialog({
	open,
	onOpenChange,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const queryClient = useQueryClient();
	const [name, setName] = useState("");
	const [type, setType] = useState<AccountType>("checking");
	const [balance, setBalance] = useState("");
	const [limit, setLimit] = useState("");
	const [apr, setApr] = useState("");
	const isDebt = type === "credit" || type === "loan";

	const create = useMutation({
		mutationFn: createAccountFn,
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: accountKeys.all });
			onOpenChange(false);
		},
		onError: (e) => toast.error(e.message),
	});

	function save() {
		const balanceCents = parseCents(balance || "0");
		if (balanceCents === null) {
			toast.error("Invalid balance");
			return;
		}
		const limitCents = limit.trim() ? parseCents(limit) : null;
		const aprBps = apr.trim() ? Math.round(Number(apr) * 100) : null;
		if (
			(limit.trim() && limitCents === null) ||
			(apr.trim() && aprBps !== null && Number.isNaN(aprBps))
		) {
			toast.error("Invalid limit or APR");
			return;
		}
		create.mutate({
			name,
			type,
			balanceCents: isDebt && balanceCents > 0 ? -balanceCents : balanceCents,
			limitCents,
			aprBps,
		});
	}

	return (
		<EntityDialog
			open={open}
			onOpenChange={onOpenChange}
			title="New Account"
			onSave={save}
			saveLabel="Create Account"
			saving={create.isPending}
		>
			<Label htmlFor="acc-name">Name</Label>
			<Input
				id="acc-name"
				value={name}
				onChange={(e) => setName(e.target.value)}
				autoFocus
			/>
			<Label htmlFor="acc-type">Type</Label>
			<Select value={type} onValueChange={(v) => setType(v as AccountType)}>
				<SelectTrigger id="acc-type">
					<SelectValue />
				</SelectTrigger>
				<SelectContent>
					{TYPES.map((t) => (
						<SelectItem key={t.value} value={t.value}>
							{t.label}
						</SelectItem>
					))}
				</SelectContent>
			</Select>
			<Label htmlFor="acc-balance">
				{isDebt ? "Current balance owed" : "Current balance"}
			</Label>
			<Input
				id="acc-balance"
				value={balance}
				onChange={(e) => setBalance(e.target.value)}
				placeholder="$0.00"
			/>
			{isDebt && (
				<>
					<Label htmlFor="acc-limit">
						{type === "credit" ? "Credit limit" : "Original principal"}
					</Label>
					<Input
						id="acc-limit"
						value={limit}
						onChange={(e) => setLimit(e.target.value)}
						placeholder="$0.00"
					/>
					<Label htmlFor="acc-apr">APR %</Label>
					<Input
						id="acc-apr"
						value={apr}
						onChange={(e) => setApr(e.target.value)}
						placeholder="21.99"
					/>
				</>
			)}
		</EntityDialog>
	);
}
