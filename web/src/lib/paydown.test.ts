import { describe, expect, it } from "vitest";
import {
	compute,
	daysInMonth,
	flatSchedule,
	type MonthPayment,
} from "./paydown.ts";

describe("daysInMonth", () => {
	it("handles month lengths and leap years", () => {
		expect(daysInMonth(new Date(Date.UTC(2026, 0, 5)))).toBe(31);
		expect(daysInMonth(new Date(Date.UTC(2026, 1, 1)))).toBe(28);
		expect(daysInMonth(new Date(Date.UTC(2024, 1, 1)))).toBe(29);
		expect(daysInMonth(new Date(Date.UTC(2026, 3, 30)))).toBe(30);
	});
});

describe("compute", () => {
	it("amortizes a real-shaped loan and pays it off", () => {
		const p = compute(
			1,
			"Visa",
			2099,
			4_285_659,
			flatSchedule(80_000, 360),
			new Date(Date.UTC(2026, 0, 1)),
		);
		expect(p.rows.length).toBeGreaterThan(0);
		expect(p.rows[0].interestCents).toBeGreaterThan(0);
		expect(p.rows[0].paymentCents).toBe(80_000);
		expect(p.rows[0].balanceCents).toBeLessThan(4_285_659);
		expect(p.payoffMonth).not.toBeNull();
		expect(p.diverging).toBe(false);
	});

	it("short-circuits a zero balance", () => {
		const p = compute(1, "X", 1000, 0, flatSchedule(10_000, 12), new Date());
		expect(p.rows.length).toBe(0);
	});

	it("detects a diverging loan when underpaying", () => {
		const p = compute(
			1,
			"Bad",
			3000,
			1_000_000,
			flatSchedule(5_000, 6),
			new Date(Date.UTC(2026, 0, 1)),
		);
		expect(p.diverging).toBe(true);
		expect(p.rows[p.rows.length - 1].balanceCents).toBeGreaterThan(
			p.startCents,
		);
	});

	it("honors a variable schedule and carries the source", () => {
		const schedule: MonthPayment[] = [
			{ cents: 50_000, source: "spent" },
			{ cents: 100_000, source: "assigned" },
			{ cents: 50_000, source: "default" },
		];
		const p = compute(
			1,
			"V",
			1200,
			1_000_000,
			schedule,
			new Date(Date.UTC(2026, 3, 1)),
		);
		expect(p.rows.length).toBe(3);
		expect(p.rows.map((r) => r.paymentCents)).toEqual([
			50_000, 100_000, 50_000,
		]);
		expect(p.rows.map((r) => r.paymentSource)).toEqual([
			"spent",
			"assigned",
			"default",
		]);
	});
});
