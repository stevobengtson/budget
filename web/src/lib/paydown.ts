// Projects monthly debt amortization for credit/loan accounts. Each month the
// balance accrues interest over the calendar month's days at APR/365 daily
// compounding, then the payment is applied. Port of Go core/paydown.

export type PaymentSource = "default" | "assigned" | "spent";

export type MonthPayment = {
	cents: number;
	source: PaymentSource;
};

export type Row = {
	month: Date;
	startCents: number;
	interestCents: number;
	paymentCents: number;
	paymentSource: PaymentSource;
	balanceCents: number;
};

export type Plan = {
	accountId: number;
	accountName: string;
	aprBps: number;
	startCents: number;
	paymentCents: number;
	rows: Row[];
	totalInterestCents: number;
	totalPaidCents: number;
	payoffMonth: Date | null; // null when not paid off within the horizon
	diverging: boolean; // payment <= first month's interest -> grows forever
};

export function daysInMonth(d: Date): number {
	return new Date(
		Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + 1, 0),
	).getUTCDate();
}

export function flatSchedule(
	paymentCents: number,
	months: number,
): MonthPayment[] {
	return Array.from({ length: months }, () => ({
		cents: paymentCents,
		source: "default" as const,
	}));
}

export function compute(
	accountId: number,
	name: string,
	aprBps: number,
	startCents: number,
	schedule: MonthPayment[],
	start: Date,
): Plan {
	if (aprBps < 0) throw new Error("apr must be non-negative");
	if (schedule.length === 0)
		throw new Error("schedule must have at least one month");

	const firstPay = schedule[0].cents;
	const plan: Plan = {
		accountId,
		accountName: name,
		aprBps,
		startCents,
		paymentCents: firstPay,
		rows: [],
		totalInterestCents: 0,
		totalPaidCents: 0,
		payoffMonth: null,
		diverging: false,
	};

	if (startCents <= 0) return plan; // already paid off

	const daily = aprBps / 10_000 / 365;
	let cur = new Date(Date.UTC(start.getUTCFullYear(), start.getUTCMonth(), 1));
	let balance = startCents;

	for (let i = 0; i < schedule.length; i++) {
		const mp = schedule[i];
		const days = daysInMonth(cur);
		const factor = (1 + daily) ** days - 1;
		const interest = Math.round(balance * factor);
		const afterInterest = balance + interest;

		if (i === 0 && mp.cents > 0 && mp.cents <= interest) {
			plan.diverging = true;
		}

		const pay = Math.min(mp.cents, afterInterest);
		const end = afterInterest - pay;

		plan.rows.push({
			month: cur,
			startCents: balance,
			interestCents: interest,
			paymentCents: pay,
			paymentSource: mp.source,
			balanceCents: end,
		});
		plan.totalInterestCents += interest;
		plan.totalPaidCents += pay;

		balance = end;
		if (balance <= 0) {
			plan.payoffMonth = cur;
			break;
		}
		cur = new Date(Date.UTC(cur.getUTCFullYear(), cur.getUTCMonth() + 1, 1));
	}
	return plan;
}
