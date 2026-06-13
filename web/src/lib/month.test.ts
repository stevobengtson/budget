import { describe, expect, it } from "vitest";
import {
	addMonths,
	formatMonth,
	isInMonth,
	monthOf,
	todayMonth,
} from "./month.ts";

describe("month utils", () => {
	it("addMonths crosses year boundaries", () => {
		expect(addMonths("2026-01", -1)).toBe("2025-12");
		expect(addMonths("2026-12", 1)).toBe("2027-01");
		expect(addMonths("2026-06", 0)).toBe("2026-06");
	});
	it("formatMonth renders long month + year", () => {
		expect(formatMonth("2026-06")).toBe("June 2026");
	});
	it("monthOf extracts month from an ISO date", () => {
		expect(monthOf("2026-06-12")).toBe("2026-06");
	});
	it("isInMonth matches only that month", () => {
		expect(isInMonth("2026-06-01", "2026-06")).toBe(true);
		expect(isInMonth("2026-05-31", "2026-06")).toBe(false);
	});
	it("todayMonth returns YYYY-MM", () => {
		expect(todayMonth()).toMatch(/^\d{4}-(0[1-9]|1[0-2])$/);
	});
});
