import { createServerFn } from "@tanstack/react-start";
import { getRequest } from "@tanstack/react-start/server";
import { requireUserId } from "@/server/auth-helpers";
import {
	type AccountWithBalance,
	type CreateAccountInput,
	createAccount,
	getAccount,
	listAccounts,
} from "@/server/db/accounts";

export type Account = AccountWithBalance;

export const fetchAccounts = createServerFn({ method: "GET" }).handler(
	async () => {
		const { headers } = getRequest();
		const userId = await requireUserId(headers);
		const accounts = await listAccounts(userId, false);
		return { accounts };
	},
);

export const fetchAccount = createServerFn({ method: "GET" })
	.inputValidator((id: number) => id)
	.handler(async ({ data: id }) => {
		const { headers } = getRequest();
		const userId = await requireUserId(headers);
		const account = await getAccount(userId, id);
		if (!account) {
			throw new Response("Not found", { status: 404 });
		}
		return account;
	});

export const createAccountFn = createServerFn({ method: "POST" })
	.inputValidator((input: CreateAccountInput) => input)
	.handler(async ({ data: input }) => {
		const { headers } = getRequest();
		const userId = await requireUserId(headers);
		const id = await createAccount(userId, input);
		const account = await getAccount(userId, id);
		if (!account) {
			throw new Response("Account created but could not be loaded", {
				status: 500,
			});
		}
		return account;
	});
