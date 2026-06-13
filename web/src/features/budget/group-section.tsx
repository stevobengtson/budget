import { PencilIcon, PlusIcon } from "lucide-react";
import { Amount } from "#/components/amount.tsx";
import { Button } from "#/components/ui/button.tsx";
import type { BudgetGroupRow } from "#/lib/api/types.ts";
import { CategoryRow } from "./category-row.tsx";

export function GroupSection({
	group,
	onAssign,
	onEditGroup,
	onAddCategory,
	onEditCategory,
}: {
	group: BudgetGroupRow;
	onAssign: (categoryId: string, cents: number) => void;
	onEditGroup: () => void;
	onAddCategory: () => void;
	onEditCategory: (categoryId: string) => void;
}) {
	return (
		<div className="overflow-hidden rounded-lg border bg-card">
			<div className="group flex items-center justify-between bg-muted/50 px-4 py-2">
				<div className="flex items-center gap-2">
					<span className="text-sm font-semibold">{group.name}</span>
					<Button
						variant="ghost"
						size="icon"
						className="size-6 opacity-0 group-hover:opacity-100"
						aria-label={`Edit group ${group.name}`}
						onClick={onEditGroup}
					>
						<PencilIcon className="size-3" />
					</Button>
					<Button
						variant="ghost"
						size="icon"
						className="size-6 opacity-0 group-hover:opacity-100"
						aria-label={`Add category to ${group.name}`}
						onClick={onAddCategory}
					>
						<PlusIcon className="size-3" />
					</Button>
				</div>
				<div className="grid grid-cols-[8rem_8rem_10rem_2rem] gap-2 text-xs text-muted-foreground">
					<span className="text-right tabular-nums">
						<Amount
							cents={group.assignedCents}
							tone="neutral"
							className="text-muted-foreground"
						/>
					</span>
					<span className="text-right tabular-nums">
						<Amount
							cents={group.activityCents}
							tone="neutral"
							className="text-muted-foreground"
						/>
					</span>
					<span className="text-right tabular-nums">
						<Amount cents={group.availableCents} />
					</span>
					<span />
				</div>
			</div>
			{group.categories.map((category) => (
				<CategoryRow
					key={category.categoryId}
					category={category}
					onAssign={(cents) => onAssign(category.categoryId, cents)}
					onEdit={() => onEditCategory(category.categoryId)}
				/>
			))}
		</div>
	);
}
