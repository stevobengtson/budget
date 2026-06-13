import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Amount } from "./amount.tsx";

describe("Amount", () => {
	it("renders positive cents in green with currency format", () => {
		render(<Amount cents={123456} />);
		const el = screen.getByText("$1,234.56");
		expect(el.className).toContain("text-emerald-600");
	});
	it("renders negative cents in destructive", () => {
		render(<Amount cents={-50000} />);
		expect(screen.getByText("-$500.00").className).toContain(
			"text-destructive",
		);
	});
	it("renders zero muted", () => {
		render(<Amount cents={0} />);
		expect(screen.getByText("$0.00").className).toContain(
			"text-muted-foreground",
		);
	});
	it("supports neutral tone override (table cells)", () => {
		render(<Amount cents={123} tone="neutral" />);
		expect(screen.getByText("$1.23").className).toContain("text-foreground");
	});
});
