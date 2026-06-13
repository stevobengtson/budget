import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { MonthNavigator } from "#/components/month-navigator.tsx";
import { BudgetScreen } from "#/features/budget/budget-screen.tsx";
import { budgetQuery } from "#/features/budget/queries.ts";
import { monthSchemaPattern, todayMonth } from "#/lib/month.ts";

const searchSchema = z.object({
	month: z
		.string()
		.regex(monthSchemaPattern)
		.catch(todayMonth)
		.default(todayMonth),
});

export const Route = createFileRoute("/_authed/budget")({
	validateSearch: searchSchema,
	loaderDeps: ({ search }) => ({ month: search.month }),
	loader: ({ context: { queryClient }, deps }) =>
		queryClient.ensureQueryData(budgetQuery(deps.month)),
	component: BudgetPage,
});

function BudgetPage() {
	const { month } = Route.useSearch();
	const navigate = Route.useNavigate();
	return (
		<div className="flex flex-1 flex-col gap-4 p-4 lg:p-6">
			<div className="flex items-center justify-between">
				<h1 className="text-xl font-semibold">Budget</h1>
				<MonthNavigator
					month={month}
					onMonthChange={(m) =>
						navigate({ search: (prev) => ({ ...prev, month: m }) })
					}
				/>
			</div>
			<BudgetScreen month={month} />
		</div>
	);
}
