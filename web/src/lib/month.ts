const MONTH_RE = /^\d{4}-(0[1-9]|1[0-2])$/;

export const monthSchemaPattern = MONTH_RE;

export function todayMonth(): string {
	const now = new Date();
	return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}`;
}

export function addMonths(month: string, delta: number): string {
	const [y, m] = month.split("-").map(Number);
	const index = y * 12 + (m - 1) + delta;
	const year = Math.floor(index / 12);
	const mon = (index % 12) + 1;
	return `${year}-${String(mon).padStart(2, "0")}`;
}

export function formatMonth(month: string): string {
	const [y, m] = month.split("-").map(Number);
	return new Date(Date.UTC(y, m - 1, 1)).toLocaleDateString("en-US", {
		month: "long",
		year: "numeric",
		timeZone: "UTC",
	});
}

export function monthOf(isoDate: string): string {
	return isoDate.slice(0, 7);
}

export function isInMonth(isoDate: string, month: string): boolean {
	return monthOf(isoDate) === month;
}

export function isValidMonth(value: string): boolean {
	return MONTH_RE.test(value);
}
