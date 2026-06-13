import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { PencilIcon, PlusIcon, Trash2Icon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { Amount } from "#/components/amount.tsx";
import { Button } from "#/components/ui/button.tsx";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogHeader,
	DialogTitle,
} from "#/components/ui/dialog.tsx";
import { Input } from "#/components/ui/input.tsx";
import type { IncomeSource } from "#/lib/api/types.ts";
import { parseCents } from "#/lib/money.ts";
import {
	budgetKeys,
	deleteIncomeSourceFn,
	incomeSourcesQuery,
	upsertIncomeSourceFn,
} from "./queries.ts";

function IncomeRowForm({
	source,
	onSave,
	onCancel,
}: {
	source?: IncomeSource;
	onSave: (input: Omit<IncomeSource, "id"> & { id?: string }) => void;
	onCancel: () => void;
}) {
	const [name, setName] = useState(source?.name ?? "");
	const [amount, setAmount] = useState(
		source ? (source.amountCents / 100).toFixed(2) : "",
	);
	const [day, setDay] = useState(source ? String(source.dayOfMonth) : "1");

	function submit() {
		const amountCents = parseCents(amount);
		if (amountCents === null) {
			toast.error("Invalid amount");
			return;
		}
		onSave({ id: source?.id, name, amountCents, dayOfMonth: Number(day) });
	}

	return (
		<div className="flex items-center gap-2 py-1.5">
			<Input
				value={name}
				onChange={(e) => setName(e.target.value)}
				placeholder="Name"
				className="h-8 flex-1"
				autoFocus
			/>
			<Input
				value={amount}
				onChange={(e) => setAmount(e.target.value)}
				placeholder="Amount"
				className="h-8 w-28 text-right"
			/>
			<Input
				value={day}
				onChange={(e) => setDay(e.target.value)}
				placeholder="Day"
				className="h-8 w-16 text-right"
			/>
			<Button size="sm" onClick={submit}>
				Save
			</Button>
			<Button size="sm" variant="ghost" onClick={onCancel}>
				Cancel
			</Button>
		</div>
	);
}

export function ManageIncomeDialog({
	open,
	onOpenChange,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const queryClient = useQueryClient();
	const { data: sources } = useQuery(incomeSourcesQuery());
	const [editingId, setEditingId] = useState<string | null>(null);
	const [adding, setAdding] = useState(false);

	const invalidate = () => {
		setEditingId(null);
		setAdding(false);
		queryClient.invalidateQueries({ queryKey: budgetKeys.income });
		queryClient.invalidateQueries({ queryKey: ["budget"] });
	};
	const upsert = useMutation({
		mutationFn: upsertIncomeSourceFn,
		onSuccess: invalidate,
		onError: (e) => toast.error(e.message),
	});
	const remove = useMutation({
		mutationFn: deleteIncomeSourceFn,
		onSuccess: invalidate,
		onError: (e) => toast.error(e.message),
	});

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="sm:max-w-lg">
				<DialogHeader>
					<DialogTitle>Manage Income</DialogTitle>
					<DialogDescription>
						Expected income sources for each month.
					</DialogDescription>
				</DialogHeader>
				<div className="divide-y">
					{(sources ?? []).map((source) =>
						editingId === source.id ? (
							<IncomeRowForm
								key={source.id}
								source={source}
								onSave={upsert.mutate}
								onCancel={() => setEditingId(null)}
							/>
						) : (
							<div
								key={source.id}
								className="flex items-center justify-between py-2"
							>
								<div className="flex flex-col">
									<span className="text-sm font-medium">{source.name}</span>
									<span className="text-xs text-muted-foreground">
										Day {source.dayOfMonth}
									</span>
								</div>
								<div className="flex items-center gap-1">
									<Amount
										cents={source.amountCents}
										tone="neutral"
										className="mr-2"
									/>
									<Button
										variant="ghost"
										size="icon"
										className="size-7"
										aria-label="Edit"
										onClick={() => setEditingId(source.id)}
									>
										<PencilIcon className="size-3.5" />
									</Button>
									<Button
										variant="ghost"
										size="icon"
										className="size-7 text-destructive"
										aria-label="Delete"
										onClick={() => remove.mutate(source.id)}
									>
										<Trash2Icon className="size-3.5" />
									</Button>
								</div>
							</div>
						),
					)}
					{adding ? (
						<IncomeRowForm
							onSave={upsert.mutate}
							onCancel={() => setAdding(false)}
						/>
					) : (
						<Button
							variant="ghost"
							size="sm"
							className="mt-2"
							onClick={() => setAdding(true)}
						>
							<PlusIcon className="size-4" /> Add income source
						</Button>
					)}
				</div>
			</DialogContent>
		</Dialog>
	);
}
