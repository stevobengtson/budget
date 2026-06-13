import { CalendarIcon, ChevronLeftIcon, ChevronRightIcon } from "lucide-react";
import { Button } from "#/components/ui/button.tsx";
import { addMonths, formatMonth, todayMonth } from "#/lib/month.ts";

export function MonthNavigator({
	month,
	onMonthChange,
}: {
	month: string;
	onMonthChange: (month: string) => void;
}) {
	return (
		<div className="flex items-center gap-1">
			<Button
				variant="ghost"
				size="icon"
				aria-label="Previous month"
				onClick={() => onMonthChange(addMonths(month, -1))}
			>
				<ChevronLeftIcon />
			</Button>
			<span className="flex min-w-28 items-center justify-center gap-2 text-sm font-medium">
				<CalendarIcon className="size-4 text-muted-foreground" />
				{formatMonth(month)}
			</span>
			<Button
				variant="ghost"
				size="icon"
				aria-label="Next month"
				onClick={() => onMonthChange(addMonths(month, 1))}
			>
				<ChevronRightIcon />
			</Button>
			<Button
				variant="outline"
				size="sm"
				disabled={month === todayMonth()}
				onClick={() => onMonthChange(todayMonth())}
			>
				Today
			</Button>
		</div>
	);
}
