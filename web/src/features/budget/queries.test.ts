import { QueryClient } from "@tanstack/react-query";
import { beforeEach, describe, expect, it } from "vitest";
import { resetDb } from "#/lib/fake/db.ts";
import { todayMonth } from "#/lib/month.ts";
import { assignBudgetFn, budgetQuery } from "./queries.ts";

beforeEach(() => resetDb());

describe("budget query plumbing", () => {
	it("fetches via ensureQueryData and refetches after invalidation", async () => {
		const queryClient = new QueryClient();
		const month = todayMonth();
		const first = await queryClient.ensureQueryData(budgetQuery(month));
		expect(first.groups.length).toBeGreaterThan(0);

		await assignBudgetFn({ month, categoryId: "cat-groceries", cents: 60000 });
		await queryClient.invalidateQueries({ queryKey: ["budget", month] });
		const second = await queryClient.fetchQuery(budgetQuery(month));
		const cat = second.groups
			.flatMap((g) => g.categories)
			.find((c) => c.categoryId === "cat-groceries");
		expect(cat?.assignedCents).toBe(60000);
	});
});
