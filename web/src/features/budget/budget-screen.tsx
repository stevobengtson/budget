import {
	useMutation,
	useQueryClient,
	useSuspenseQuery,
} from "@tanstack/react-query";
import { PlusIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { Button } from "#/components/ui/button.tsx";
import { BudgetSummaryBar } from "./budget-summary-bar.tsx";
import {
	CategoryDialog,
	type CategoryDialogState,
} from "./category-dialogs.tsx";
import { BUDGET_GRID } from "./category-row.tsx";
import { GroupDialog, type GroupDialogState } from "./group-dialogs.tsx";
import { GroupSection } from "./group-section.tsx";
import { IncomeCard } from "./income-card.tsx";
import { assignBudgetFn, budgetQuery } from "./queries.ts";

export function BudgetScreen({ month }: { month: string }) {
	const { data: budget } = useSuspenseQuery(budgetQuery(month));
	const queryClient = useQueryClient();
	const [groupDialog, setGroupDialog] = useState<GroupDialogState>(null);
	const [categoryDialog, setCategoryDialog] =
		useState<CategoryDialogState>(null);

	const assignMutation = useMutation({
		mutationFn: assignBudgetFn,
		onSuccess: () => queryClient.invalidateQueries({ queryKey: ["budget"] }),
		onError: (e) => toast.error(e.message),
	});

	return (
		<div className="flex flex-col gap-4">
			<BudgetSummaryBar budget={budget} />
			<IncomeCard />
			<div
				className={`${BUDGET_GRID} items-center gap-2 px-4 text-xs font-medium text-muted-foreground`}
			>
				<span>Category</span>
				<span className="text-right">Budgeted</span>
				<span className="text-right">Spent</span>
				<span className="text-right">Available</span>
				<span />
			</div>
			{budget.groups.map((group) => (
				<GroupSection
					key={group.groupId}
					group={group}
					onAssign={(categoryId, cents) =>
						assignMutation.mutate({ month, categoryId, cents })
					}
					onEditGroup={() =>
						setGroupDialog({ mode: "edit", groupId: group.groupId })
					}
					onAddCategory={() =>
						setCategoryDialog({ mode: "create", groupId: group.groupId })
					}
					onEditCategory={(categoryId) =>
						setCategoryDialog({ mode: "edit", categoryId })
					}
				/>
			))}
			<Button
				variant="outline"
				className="self-start"
				onClick={() => setGroupDialog({ mode: "create" })}
			>
				<PlusIcon className="size-4" /> New Group
			</Button>
			<GroupDialog state={groupDialog} onClose={() => setGroupDialog(null)} />
			<CategoryDialog
				state={categoryDialog}
				onClose={() => setCategoryDialog(null)}
			/>
		</div>
	);
}
