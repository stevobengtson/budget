import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
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
import { parseCents } from "#/lib/money.ts";
import {
	categoriesQuery,
	createCategoryFn,
	deleteCategoryFn,
	groupsQuery,
	updateCategoryFn,
} from "./queries.ts";

export type CategoryDialogState =
	| { mode: "create"; groupId: string }
	| { mode: "edit"; categoryId: string }
	| null;

export function CategoryDialog({
	state,
	onClose,
}: {
	state: CategoryDialogState;
	onClose: () => void;
}) {
	const queryClient = useQueryClient();
	const { data: groups } = useQuery(groupsQuery());
	const { data: categories } = useQuery(categoriesQuery());
	const editing =
		state?.mode === "edit"
			? categories?.find((c) => c.id === state.categoryId)
			: undefined;

	const [name, setName] = useState("");
	const [groupId, setGroupId] = useState("");
	const [goal, setGoal] = useState("");

	useEffect(() => {
		if (state?.mode === "edit") {
			setName(editing?.name ?? "");
			setGroupId(editing?.groupId ?? "");
			setGoal(
				editing?.goalCents != null ? (editing.goalCents / 100).toFixed(2) : "",
			);
		} else if (state?.mode === "create") {
			setName("");
			setGroupId(state.groupId);
			setGoal("");
		}
	}, [state, editing]);

	const invalidate = () => {
		queryClient.invalidateQueries({ queryKey: ["budget"] });
		onClose();
	};
	const create = useMutation({
		mutationFn: createCategoryFn,
		onSuccess: invalidate,
		onError: (e) => toast.error(e.message),
	});
	const update = useMutation({
		mutationFn: updateCategoryFn,
		onSuccess: invalidate,
		onError: (e) => toast.error(e.message),
	});
	const remove = useMutation({
		mutationFn: deleteCategoryFn,
		onSuccess: invalidate,
		onError: (e) => toast.error(e.message),
	});

	if (!state) return null;

	function save() {
		const goalCents = goal.trim() ? parseCents(goal) : null;
		if (goal.trim() && goalCents === null) {
			toast.error("Invalid goal amount");
			return;
		}
		if (state?.mode === "create") {
			create.mutate({ groupId, name, goalCents });
		} else if (state?.mode === "edit") {
			update.mutate({
				id: state.categoryId,
				patch: { groupId, name, goalCents },
			});
		}
	}

	return (
		<EntityDialog
			open
			onOpenChange={(open) => !open && onClose()}
			title={state.mode === "create" ? "Create Category" : "Edit Category"}
			onSave={save}
			onDelete={
				state.mode === "edit"
					? () => remove.mutate(state.categoryId)
					: undefined
			}
			saving={create.isPending || update.isPending}
		>
			<Label htmlFor="cat-name">Name</Label>
			<Input
				id="cat-name"
				value={name}
				onChange={(e) => setName(e.target.value)}
				autoFocus
			/>
			<Label htmlFor="cat-group">Group</Label>
			<Select value={groupId} onValueChange={setGroupId}>
				<SelectTrigger id="cat-group">
					<SelectValue placeholder="Group" />
				</SelectTrigger>
				<SelectContent>
					{(groups ?? []).map((g) => (
						<SelectItem key={g.id} value={g.id}>
							{g.name}
						</SelectItem>
					))}
				</SelectContent>
			</Select>
			<Label htmlFor="cat-goal">Monthly goal (optional)</Label>
			<Input
				id="cat-goal"
				value={goal}
				onChange={(e) => setGoal(e.target.value)}
				placeholder="$0.00"
			/>
		</EntityDialog>
	);
}
