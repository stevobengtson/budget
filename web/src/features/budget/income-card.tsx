import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Amount } from "#/components/amount.tsx";
import { Button } from "#/components/ui/button.tsx";
import { ManageIncomeDialog } from "./manage-income-dialog.tsx";
import { incomeSourcesQuery } from "./queries.ts";

export function IncomeCard() {
	const { data: sources } = useQuery(incomeSourcesQuery());
	const [open, setOpen] = useState(false);
	const totalCents = (sources ?? []).reduce((s, i) => s + i.amountCents, 0);

	return (
		<div className="flex items-center justify-between rounded-lg border bg-card px-4 py-3">
			<div className="flex flex-col">
				<span className="text-sm font-medium">Expected income</span>
				<span className="text-xs text-muted-foreground">
					{(sources ?? []).map((s) => s.name).join(", ") || "No income sources"}
				</span>
			</div>
			<div className="flex items-center gap-3">
				<Amount cents={totalCents} className="text-lg font-semibold" />
				<Button variant="outline" size="sm" onClick={() => setOpen(true)}>
					Manage Income
				</Button>
			</div>
			<ManageIncomeDialog open={open} onOpenChange={setOpen} />
		</div>
	);
}
