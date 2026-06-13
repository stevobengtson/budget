import { PencilIcon } from "lucide-react";
import { Amount } from "#/components/amount.tsx";
import { EditableCurrencyCell } from "#/components/editable-currency-cell.tsx";
import { Button } from "#/components/ui/button.tsx";
import { Progress } from "#/components/ui/progress.tsx";
import type { BudgetCategoryRow } from "#/lib/api/types.ts";

/** Shared grid template — keep CategoryRow and the labels row aligned. */
export const BUDGET_GRID = "grid grid-cols-[1fr_8rem_8rem_10rem_2rem]";

export function CategoryRow({
	category,
	onAssign,
	onEdit,
}: {
	category: BudgetCategoryRow;
	onAssign: (cents: number) => void;
	onEdit: () => void;
}) {
	const goalProgress =
		category.goalCents && category.goalCents > 0
			? Math.min(
					100,
					Math.round((category.assignedCents / category.goalCents) * 100),
				)
			: null;

	return (
		<div
			className={`group ${BUDGET_GRID} items-center gap-2 border-b px-4 py-2 text-sm hover:bg-accent/40`}
		>
			<span className="truncate">{category.name}</span>
			<EditableCurrencyCell
				cents={category.assignedCents}
				onSave={onAssign}
				ariaLabel={`${category.name} budgeted amount`}
			/>
			<span className="text-right">
				<Amount
					cents={category.activityCents}
					tone="neutral"
					className="text-muted-foreground"
				/>
			</span>
			<div className="flex flex-col items-end gap-1">
				<Amount cents={category.availableCents} />
				{goalProgress !== null && (
					<Progress value={goalProgress} className="h-1 w-full" />
				)}
			</div>
			<Button
				variant="ghost"
				size="icon"
				className="size-6 opacity-0 group-hover:opacity-100"
				aria-label={`Edit ${category.name}`}
				onClick={onEdit}
			>
				<PencilIcon className="size-3" />
			</Button>
		</div>
	);
}
