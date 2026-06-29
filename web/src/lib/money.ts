const usd = new Intl.NumberFormat("en-US", {
	style: "currency",
	currency: "USD",
});

export function formatCents(cents: number): string {
	return usd.format(cents / 100);
}

/** Parse user input ("$1,234.56") to integer cents. Returns null when not a
 * number. Rejects more than two fractional digits, matching Go money.Parse. */
export function parseCents(input: string): number | null {
	const cleaned = input.replace(/[$,\s]/g, "");
	if (cleaned === "" || cleaned === "-" || cleaned === "+") return null;
	const dot = cleaned.indexOf(".");
	if (dot !== -1 && cleaned.length - dot - 1 > 2) return null;
	const value = Number(cleaned);
	if (!Number.isFinite(value)) return null;
	return Math.round(value * 100);
}
