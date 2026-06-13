import { queryOptions } from "@tanstack/react-query";
import type { IncomeSource } from "#/lib/api/types.ts";
import {
	assignBudget,
	type CategoryInput,
	createCategory,
	createGroup,
	deleteCategory,
	deleteGroup,
	deleteIncomeSource,
	getBudgetMonth,
	listCategories,
	listGroups,
	listIncomeSources,
	updateCategory,
	updateGroup,
	upsertIncomeSource,
} from "#/lib/fake/db.ts";
import { fakeLatency } from "#/lib/fake/delay.ts";

export const budgetKeys = {
	month: (month: string) => ["budget", month] as const,
	groups: ["budget", "groups"] as const,
	categories: ["budget", "categories"] as const,
	income: ["budget", "income"] as const,
};

export const budgetQuery = (month: string) =>
	queryOptions({
		queryKey: budgetKeys.month(month),
		queryFn: async () => {
			await fakeLatency();
			return getBudgetMonth(month);
		},
	});

export const groupsQuery = () =>
	queryOptions({
		queryKey: budgetKeys.groups,
		queryFn: async () => {
			await fakeLatency();
			return listGroups();
		},
	});

export const categoriesQuery = () =>
	queryOptions({
		queryKey: budgetKeys.categories,
		queryFn: async () => {
			await fakeLatency();
			return listCategories();
		},
	});

export const incomeSourcesQuery = () =>
	queryOptions({
		queryKey: budgetKeys.income,
		queryFn: async () => {
			await fakeLatency();
			return listIncomeSources();
		},
	});

export async function assignBudgetFn(args: {
	month: string;
	categoryId: string;
	cents: number;
}) {
	await fakeLatency();
	assignBudget(args.month, args.categoryId, args.cents);
}

export async function createGroupFn(name: string) {
	await fakeLatency();
	return createGroup(name);
}

export async function updateGroupFn(args: { id: string; name: string }) {
	await fakeLatency();
	return updateGroup(args.id, args.name);
}

export async function deleteGroupFn(id: string) {
	await fakeLatency();
	deleteGroup(id);
}

export async function createCategoryFn(input: CategoryInput) {
	await fakeLatency();
	return createCategory(input);
}

export async function updateCategoryFn(args: {
	id: string;
	patch: Partial<CategoryInput>;
}) {
	await fakeLatency();
	return updateCategory(args.id, args.patch);
}

export async function deleteCategoryFn(id: string) {
	await fakeLatency();
	deleteCategory(id);
}

export async function upsertIncomeSourceFn(
	input: Omit<IncomeSource, "id"> & { id?: string },
) {
	await fakeLatency();
	return upsertIncomeSource(input);
}

export async function deleteIncomeSourceFn(id: string) {
	await fakeLatency();
	deleteIncomeSource(id);
}
