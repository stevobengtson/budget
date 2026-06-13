import { formatCents } from "#/lib/money.ts";
import { cn } from "#/lib/utils.ts";

type Tone = "semantic" | "neutral";

export function Amount({
	cents,
	tone = "semantic",
	className,
}: {
	cents: number;
	tone?: Tone;
	className?: string;
}) {
	const color =
		tone === "neutral"
			? "text-foreground"
			: cents > 0
				? "text-emerald-600 dark:text-emerald-400"
				: cents < 0
					? "text-destructive"
					: "text-muted-foreground";
	return (
		<span className={cn("tabular-nums", color, className)}>
			{formatCents(cents)}
		</span>
	);
}
