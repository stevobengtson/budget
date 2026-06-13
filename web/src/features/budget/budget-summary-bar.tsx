import { Amount } from "#/components/amount.tsx";
import { StatCard } from "#/components/stat-card.tsx";
import type { BudgetMonth } from "#/lib/api/types.ts";

export function BudgetSummaryBar({ budget }: { budget: BudgetMonth }) {
	return (
		<div className="grid grid-cols-2 gap-3 md:grid-cols-5">
			<StatCard label="Income">
				<Amount cents={budget.incomeCents} tone="neutral" />
			</StatCard>
			<StatCard label="Assigned">
				<Amount cents={budget.assignedCents} tone="neutral" />
			</StatCard>
			<StatCard label="Remaining to assign">
				<Amount cents={budget.remainingCents} />
			</StatCard>
			<StatCard label="Activity">
				<Amount cents={budget.activityCents} />
			</StatCard>
			<StatCard label="Available">
				<Amount cents={budget.availableCents} />
			</StatCard>
		</div>
	);
}
