import { useStore } from "@tanstack/react-store";
import {
	Dialog,
	DialogContent,
	DialogHeader,
	DialogTitle,
} from "#/components/ui/dialog.tsx";
import { hotkeysRegistry } from "#/lib/hotkeys.ts";

const KEY_LABEL: Record<string, string> = {
	"mod+k": "⌘K",
	arrowleft: "←",
	arrowright: "→",
};

export function HotkeysHelpDialog({
	open,
	onOpenChange,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const entries = useStore(hotkeysRegistry);
	const groups = [...new Set(entries.map((e) => e.group))];

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="sm:max-w-md">
				<DialogHeader>
					<DialogTitle>Keyboard shortcuts</DialogTitle>
				</DialogHeader>
				<div className="flex flex-col gap-4">
					{groups.map((group) => (
						<div key={group}>
							<h3 className="mb-1 text-xs font-semibold uppercase text-muted-foreground">
								{group}
							</h3>
							{entries
								.filter((e) => e.group === group)
								.map((e) => (
									<div
										key={`${group}-${e.key}-${e.label}`}
										className="flex items-center justify-between py-1 text-sm"
									>
										<span>{e.label}</span>
										<kbd className="rounded border bg-muted px-1.5 py-0.5 font-mono text-xs">
											{KEY_LABEL[e.key] ?? e.key}
										</kbd>
									</div>
								))}
						</div>
					))}
				</div>
			</DialogContent>
		</Dialog>
	);
}
