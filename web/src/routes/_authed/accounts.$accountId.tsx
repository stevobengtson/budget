import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { MonthNavigator } from "#/components/month-navigator.tsx";
import { AccountSummaryCard } from "#/features/accounts/account-summary-card.tsx";
import {
	accountQuery,
	accountSummaryQuery,
	accountsQuery,
} from "#/features/accounts/queries.ts";
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

export const Route = createFileRoute("/_authed/accounts/$accountId")({
	validateSearch: searchSchema,
	loaderDeps: ({ search }) => ({ month: search.month }),
	loader: async ({ context: { queryClient }, deps, params }) => {
		await Promise.all([
			queryClient.ensureQueryData(accountQuery(Number(params.accountId))),
			queryClient.ensureQueryData(accountsQuery()),
			queryClient.ensureQueryData(categoriesQuery()),
			queryClient.ensureQueryData(
				accountSummaryQuery(Number(params.accountId), deps.month),
			),
			queryClient.ensureQueryData(
				transactionsQuery({ month: deps.month, accountId: params.accountId }),
			),
		]);
	},
	component: AccountTransactionsPage,
});

function AccountTransactionsPage() {
	const { accountId } = Route.useParams();
	const { month, filter } = Route.useSearch();
	const navigate = Route.useNavigate();
	const { data: account } = useSuspenseQuery(accountQuery(Number(accountId)));
	const { data: summary } = useSuspenseQuery(
		accountSummaryQuery(Number(accountId), month),
	);

	return (
		<div className="flex flex-1 flex-col gap-4 p-4 lg:p-6">
			<div className="flex items-center justify-between">
				<h1 className="text-xl font-semibold">{account.name}</h1>
				<MonthNavigator
					month={month}
					onMonthChange={(m) =>
						navigate({ search: (prev) => ({ ...prev, month: m }) })
					}
				/>
			</div>
			<AccountSummaryCard account={account} summary={summary} />
			<TransactionsScreen
				month={month}
				accountId={accountId}
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
