import type * as React from "react";
import { cn } from "#/lib/utils.ts";

export function StatCard({
	label,
	children,
	className,
}: {
	label: string;
	children: React.ReactNode;
	className?: string;
}) {
	return (
		<div
			className={cn(
				"flex flex-col gap-1 rounded-lg border bg-card px-4 py-3",
				className,
			)}
		>
			<span className="text-xs font-medium text-muted-foreground">{label}</span>
			<span className="text-xl font-semibold tabular-nums">{children}</span>
		</div>
	);
}
