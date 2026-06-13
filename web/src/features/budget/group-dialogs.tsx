import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { EntityDialog } from "#/components/entity-dialog.tsx";
import { Input } from "#/components/ui/input.tsx";
import { Label } from "#/components/ui/label.tsx";
import {
	createGroupFn,
	deleteGroupFn,
	groupsQuery,
	updateGroupFn,
} from "./queries.ts";

export type GroupDialogState =
	| { mode: "create" }
	| { mode: "edit"; groupId: string }
	| null;

export function GroupDialog({
	state,
	onClose,
}: {
	state: GroupDialogState;
	onClose: () => void;
}) {
	const queryClient = useQueryClient();
	const { data: groups } = useQuery(groupsQuery());
	const editing =
		state?.mode === "edit"
			? groups?.find((g) => g.id === state.groupId)
			: undefined;
	const [name, setName] = useState("");

	useEffect(() => {
		setName(editing?.name ?? "");
	}, [editing]);

	const invalidate = () => {
		queryClient.invalidateQueries({ queryKey: ["budget"] });
		onClose();
	};
	const create = useMutation({
		mutationFn: createGroupFn,
		onSuccess: invalidate,
		onError: (e) => toast.error(e.message),
	});
	const update = useMutation({
		mutationFn: updateGroupFn,
		onSuccess: invalidate,
		onError: (e) => toast.error(e.message),
	});
	const remove = useMutation({
		mutationFn: deleteGroupFn,
		onSuccess: invalidate,
		onError: (e) => toast.error(e.message),
	});

	if (!state) return null;
	const submit = () =>
		state.mode === "create"
			? create.mutate(name)
			: update.mutate({ id: state.groupId, name });

	return (
		<EntityDialog
			open
			onOpenChange={(open) => !open && onClose()}
			title={state.mode === "create" ? "Create Group" : "Edit Group"}
			onSave={submit}
			onDelete={
				state.mode === "edit" ? () => remove.mutate(state.groupId) : undefined
			}
			saving={create.isPending || update.isPending}
		>
			<Label htmlFor="group-name">Name</Label>
			<Input
				id="group-name"
				value={name}
				onChange={(e) => setName(e.target.value)}
				autoFocus
				onKeyDown={(e) => e.key === "Enter" && submit()}
			/>
		</EntityDialog>
	);
}
