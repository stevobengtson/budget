import type * as React from "react";
import { Button } from "#/components/ui/button.tsx";
import {
	Dialog,
	DialogContent,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "#/components/ui/dialog.tsx";

export function EntityDialog({
	open,
	onOpenChange,
	title,
	children,
	onSave,
	saveLabel = "Save",
	onDelete,
	deleteLabel = "Delete",
	saving = false,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	title: string;
	children: React.ReactNode;
	onSave: () => void;
	saveLabel?: string;
	onDelete?: () => void;
	deleteLabel?: string;
	saving?: boolean;
}) {
	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="sm:max-w-sm">
				<DialogHeader>
					<DialogTitle>{title}</DialogTitle>
				</DialogHeader>
				<div className="flex flex-col gap-3">{children}</div>
				<DialogFooter className="flex items-center sm:justify-between">
					{onDelete ? (
						<Button
							variant="ghost"
							className="text-destructive"
							onClick={onDelete}
						>
							{deleteLabel}
						</Button>
					) : (
						<span />
					)}
					<div className="flex gap-2">
						<Button variant="outline" onClick={() => onOpenChange(false)}>
							Cancel
						</Button>
						<Button onClick={onSave} disabled={saving}>
							{saveLabel}
						</Button>
					</div>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
