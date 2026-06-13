import { describe, expect, it } from "vitest";
import { formatCents, parseCents } from "./money.ts";

describe("formatCents", () => {
	it("formats positive cents as USD", () => {
		expect(formatCents(615000)).toBe("$6,150.00");
	});
	it("formats negative cents", () => {
		expect(formatCents(-147153)).toBe("-$1,471.53");
	});
	it("formats zero", () => {
		expect(formatCents(0)).toBe("$0.00");
	});
});

describe("parseCents", () => {
	it("parses plain decimals", () => {
		expect(parseCents("1234.56")).toBe(123456);
	});
	it("parses with $ and commas and spaces", () => {
		expect(parseCents(" $1,234.56 ")).toBe(123456);
	});
	it("parses integers as dollars", () => {
		expect(parseCents("45")).toBe(4500);
	});
	it("parses negatives", () => {
		expect(parseCents("-12.30")).toBe(-1230);
	});
	it("rounds half-cent float artifacts", () => {
		expect(parseCents("0.1")).toBe(10);
		expect(parseCents("19.99")).toBe(1999);
	});
	it("returns null for junk or empty", () => {
		expect(parseCents("")).toBeNull();
		expect(parseCents("abc")).toBeNull();
		expect(parseCents("12.3.4")).toBeNull();
	});
});
