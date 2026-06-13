import type {
	Account,
	Category,
	CategoryGroup,
	IncomeSource,
	Transaction,
} from "../api/types.ts";
import { addMonths, todayMonth } from "../month.ts";

export interface AccountSeed extends Account {
	openingBalanceCents: number;
}

export const accountSeeds: AccountSeed[] = [
	{
		id: "acc-checking",
		name: "Main Checking",
		type: "checking",
		balanceCents: 0,
		openingBalanceCents: 412500,
		limitCents: null,
		aprBps: null,
	},
	{
		id: "acc-savings",
		name: "High-Yield Savings",
		type: "savings",
		balanceCents: 0,
		openingBalanceCents: 1250000,
		limitCents: null,
		aprBps: 425,
	},
	{
		id: "acc-credit",
		name: "Chase Sapphire",
		type: "credit",
		balanceCents: 0,
		openingBalanceCents: -84200,
		limitCents: 1200000,
		aprBps: 2199,
	},
	{
		id: "acc-loan",
		name: "Honda Car Loan",
		type: "loan",
		balanceCents: 0,
		openingBalanceCents: -972000,
		limitCents: 1850000,
		aprBps: 549,
	},
];

export const groupSeeds: CategoryGroup[] = [
	{ id: "grp-housing", name: "Housing", sortOrder: 1 },
	{ id: "grp-food", name: "Food", sortOrder: 2 },
	{ id: "grp-transport", name: "Transportation", sortOrder: 3 },
	{ id: "grp-health", name: "Health & Fitness", sortOrder: 4 },
	{ id: "grp-fun", name: "Entertainment", sortOrder: 5 },
	{ id: "grp-misc", name: "Misc", sortOrder: 6 },
	{ id: "grp-debt", name: "Debt", sortOrder: 7 },
];

export const categorySeeds: Category[] = [
	{
		id: "cat-rent",
		groupId: "grp-housing",
		name: "Rent",
		goalCents: 145000,
		locked: false,
	},
	{
		id: "cat-electric",
		groupId: "grp-housing",
		name: "Electricity",
		goalCents: 9000,
		locked: false,
	},
	{
		id: "cat-water",
		groupId: "grp-housing",
		name: "Water",
		goalCents: 5000,
		locked: false,
	},
	{
		id: "cat-internet",
		groupId: "grp-housing",
		name: "Internet & Cable",
		goalCents: 8000,
		locked: false,
	},
	{
		id: "cat-groceries",
		groupId: "grp-food",
		name: "Groceries",
		goalCents: 50000,
		locked: false,
	},
	{
		id: "cat-restaurants",
		groupId: "grp-food",
		name: "Restaurants",
		goalCents: 20000,
		locked: false,
	},
	{
		id: "cat-coffee",
		groupId: "grp-food",
		name: "Coffee Shops",
		goalCents: 6000,
		locked: false,
	},
	{
		id: "cat-gas",
		groupId: "grp-transport",
		name: "Gas",
		goalCents: 16000,
		locked: false,
	},
	{
		id: "cat-car-ins",
		groupId: "grp-transport",
		name: "Car Insurance",
		goalCents: 14200,
		locked: false,
	},
	{
		id: "cat-parking",
		groupId: "grp-transport",
		name: "Parking",
		goalCents: 4000,
		locked: false,
	},
	{
		id: "cat-gym",
		groupId: "grp-health",
		name: "Gym",
		goalCents: 5500,
		locked: false,
	},
	{
		id: "cat-medical",
		groupId: "grp-health",
		name: "Medical",
		goalCents: 10000,
		locked: false,
	},
	{
		id: "cat-streaming",
		groupId: "grp-fun",
		name: "Streaming",
		goalCents: 4500,
		locked: false,
	},
	{
		id: "cat-date",
		groupId: "grp-fun",
		name: "Date Night",
		goalCents: 15000,
		locked: false,
	},
	{
		id: "cat-emergency",
		groupId: "grp-misc",
		name: "Emergency Fund",
		goalCents: 25000,
		locked: false,
	},
	{
		id: "cat-clothing",
		groupId: "grp-misc",
		name: "Clothing",
		goalCents: 8000,
		locked: false,
	},
	{
		id: "cat-gifts",
		groupId: "grp-misc",
		name: "Gifts",
		goalCents: 5000,
		locked: false,
	},
	{
		id: "cat-cc-payment",
		groupId: "grp-debt",
		name: "Credit Card Payment",
		goalCents: 50000,
		locked: false,
	},
	{
		id: "cat-car-payment",
		groupId: "grp-debt",
		name: "Car Loan Payment",
		goalCents: 38500,
		locked: false,
	},
];

export const incomeSeeds: IncomeSource[] = [
	{ id: "inc-salary", name: "Salary", amountCents: 520000, dayOfMonth: 1 },
	{ id: "inc-side", name: "Freelance", amountCents: 95000, dayOfMonth: 15 },
];

/** Repeated each month: day-of-month + payee + category + outflow cents + account. */
const monthlySpendSeeds: ReadonlyArray<
	readonly [
		day: number,
		payee: string,
		categoryId: string,
		outflowCents: number,
		accountId: string,
	]
> = [
	[1, "Sunrise Property Mgmt", "cat-rent", 145000, "acc-checking"],
	[2, "City Power & Light", "cat-electric", 8742, "acc-checking"],
	[3, "Metro Water", "cat-water", 4310, "acc-checking"],
	[4, "Comcast", "cat-internet", 7999, "acc-checking"],
	[5, "Whole Foods", "cat-groceries", 14267, "acc-credit"],
	[6, "Blue Bottle", "cat-coffee", 1245, "acc-credit"],
	[8, "Shell", "cat-gas", 5230, "acc-credit"],
	[9, "Equinox", "cat-gym", 5500, "acc-checking"],
	[10, "Trader Joe's", "cat-groceries", 9824, "acc-credit"],
	[11, "Netflix", "cat-streaming", 1599, "acc-credit"],
	[12, "Olive Garden", "cat-restaurants", 6420, "acc-credit"],
	[13, "Spotify", "cat-streaming", 1199, "acc-credit"],
	[15, "Geico", "cat-car-ins", 14200, "acc-checking"],
	[16, "Safeway", "cat-groceries", 11650, "acc-credit"],
	[17, "Chipotle", "cat-restaurants", 2890, "acc-credit"],
	[18, "Starbucks", "cat-coffee", 985, "acc-credit"],
	[19, "Chevron", "cat-gas", 4875, "acc-credit"],
	[20, "CVS Pharmacy", "cat-medical", 3240, "acc-checking"],
	[21, "AMC Theatres", "cat-date", 4350, "acc-credit"],
	[22, "Costco", "cat-groceries", 18730, "acc-credit"],
	[23, "Uniqlo", "cat-clothing", 6480, "acc-credit"],
	[25, "Sushi Ran", "cat-date", 9875, "acc-credit"],
	[26, "Downtown Garage", "cat-parking", 1800, "acc-credit"],
	[27, "Amazon", "cat-gifts", 4523, "acc-credit"],
] as const;

export function buildTransactionSeeds(): Transaction[] {
	const months = [-2, -1, 0].map((delta) => addMonths(todayMonth(), delta));
	const today = new Date();
	const currentDay = today.getDate();
	const txs: Transaction[] = [];
	let n = 0;

	for (const [mi, month] of months.entries()) {
		const isCurrent = mi === 2;

		// income deposits
		for (const inc of incomeSeeds) {
			if (isCurrent && inc.dayOfMonth > currentDay) continue;
			txs.push({
				id: `tx-${++n}`,
				accountId: "acc-checking",
				date: `${month}-${String(inc.dayOfMonth).padStart(2, "0")}`,
				payee: inc.name,
				categoryId: "cat-income",
				transferAccountId: null,
				memo: "",
				outflowCents: 0,
				inflowCents: inc.amountCents,
				cleared: true,
			});
		}

		// spending
		for (const [
			day,
			payee,
			categoryId,
			outflowCents,
			accountId,
		] of monthlySpendSeeds) {
			if (isCurrent && day > currentDay) continue;
			txs.push({
				id: `tx-${++n}`,
				accountId,
				date: `${month}-${String(day).padStart(2, "0")}`,
				payee,
				categoryId,
				transferAccountId: null,
				memo: "",
				outflowCents,
				inflowCents: 0,
				// current month's last few days arrive uncleared
				cleared: !(isCurrent && currentDay - day < 3),
			});
		}

		// transfers: credit card payment + loan payment from checking
		const transferDay = 14;
		if (!isCurrent || transferDay <= currentDay) {
			txs.push({
				id: `tx-${++n}`,
				accountId: "acc-checking",
				date: `${month}-14`,
				payee: "Transfer : Chase Sapphire",
				categoryId: "cat-cc-payment",
				transferAccountId: "acc-credit",
				memo: "",
				outflowCents: 50000,
				inflowCents: 0,
				cleared: true,
			});
			txs.push({
				id: `tx-${++n}`,
				accountId: "acc-credit",
				date: `${month}-14`,
				payee: "Transfer : Main Checking",
				categoryId: null,
				transferAccountId: "acc-checking",
				memo: "",
				outflowCents: 0,
				inflowCents: 50000,
				cleared: true,
			});
			txs.push({
				id: `tx-${++n}`,
				accountId: "acc-checking",
				date: `${month}-14`,
				payee: "Transfer : Honda Car Loan",
				categoryId: "cat-car-payment",
				transferAccountId: "acc-loan",
				memo: "",
				outflowCents: 38500,
				inflowCents: 0,
				cleared: true,
			});
			txs.push({
				id: `tx-${++n}`,
				accountId: "acc-loan",
				date: `${month}-14`,
				payee: "Transfer : Main Checking",
				categoryId: null,
				transferAccountId: "acc-checking",
				memo: "",
				outflowCents: 0,
				inflowCents: 38500,
				cleared: true,
			});
		}
	}
	return txs;
}

/** Default monthly assignment per category = its goal. */
export function buildAssignmentSeeds(): Map<string, Map<string, number>> {
	const months = [-2, -1, 0].map((delta) => addMonths(todayMonth(), delta));
	const byMonth = new Map<string, Map<string, number>>();
	for (const month of months) {
		const m = new Map<string, number>();
		for (const cat of categorySeeds) {
			if (cat.goalCents !== null) m.set(cat.id, cat.goalCents);
		}
		byMonth.set(month, m);
	}
	return byMonth;
}
