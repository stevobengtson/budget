import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { MonthNavigator } from "#/components/month-navigator.tsx";
import { accountsQuery } from "#/features/accounts/queries.ts";
import { categoriesQuery } from "#/features/budget/queries.ts";
import { transactionsQuery } from "#/features/transactions/queries.ts";
import { TransactionsScreen } from "#/features/transactions/transactions-screen.tsx";
import { monthSchemaPattern, todayMonth } from "#/lib/month.ts";

const searchSchema = z.object({
	month: z
		.string()
		.regex(monthSchemaPattern)
		.catch(todayMonth)
		.default(todayMonth),
	filter: z.string().optional().catch(undefined),
});

export const Route = createFileRoute("/_authed/transactions")({
	validateSearch: searchSchema,
	loaderDeps: ({ search }) => ({ month: search.month }),
	loader: ({ context: { queryClient }, deps }) =>
		Promise.all([
			queryClient.ensureQueryData(transactionsQuery({ month: deps.month })),
			queryClient.ensureQueryData(accountsQuery()),
			queryClient.ensureQueryData(categoriesQuery()),
		]),
	component: TransactionsPage,
});

function TransactionsPage() {
	const { month, filter } = Route.useSearch();
	const navigate = Route.useNavigate();
	return (
		<div className="flex flex-1 flex-col gap-4 p-4 lg:p-6">
			<div className="flex items-center justify-between">
				<h1 className="text-xl font-semibold">All Accounts</h1>
				<MonthNavigator
					month={month}
					onMonthChange={(m) =>
						navigate({ search: (prev) => ({ ...prev, month: m }) })
					}
				/>
			</div>
			<TransactionsScreen
				month={month}
				filter={filter ?? ""}
				onFilterChange={(value) =>
					navigate({
						search: (prev) => ({ ...prev, filter: value || undefined }),
						replace: true,
					})
				}
			/>
		</div>
	);
}
