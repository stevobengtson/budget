import { Amount } from "#/components/amount.tsx";
import { StatCard } from "#/components/stat-card.tsx";
import type { Account, AccountMonthSummary } from "#/lib/api/types.ts";

const TYPE_LABEL: Record<Account["type"], string> = {
	checking: "Checking",
	savings: "Savings",
	credit: "Credit Card",
	loan: "Loan",
};

export function AccountSummaryCard({
	account,
	summary,
}: {
	account: Account;
	summary: AccountMonthSummary;
}) {
	return (
		<div className="grid grid-cols-2 gap-3 md:grid-cols-4">
			<StatCard label={`Balance · ${TYPE_LABEL[account.type]}`}>
				<Amount cents={summary.balanceCents} />
			</StatCard>
			<StatCard label="Inflow this month">
				<Amount cents={summary.inflowCents} />
			</StatCard>
			<StatCard label="Outflow this month">
				<Amount cents={-summary.outflowCents} />
			</StatCard>
			<StatCard label="Net">
				<Amount cents={summary.netCents} />
			</StatCard>
		</div>
	);
}
