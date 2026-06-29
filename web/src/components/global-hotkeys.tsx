import { useQuery } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { accountsQuery } from "#/features/accounts/queries.ts";
import { useAppHotkeys } from "#/lib/hotkeys.ts";
import { addMonths, todayMonth } from "#/lib/month.ts";

export function GlobalHotkeys({
	onTogglePalette,
	onToggleHelp,
}: {
	onTogglePalette: () => void;
	onToggleHelp: () => void;
}) {
	const navigate = useNavigate();
	const search = useSearch({ strict: false }) as { month?: string };
	const { data: accounts } = useQuery(accountsQuery());
	const month = search.month ?? todayMonth();

	const goMonth = (m: string) =>
		navigate({
			to: ".",
			search: (prev: Record<string, unknown>) => ({ ...prev, month: m }),
		});

	useAppHotkeys([
		{
			key: "1",
			handler: () => navigate({ to: "/budget" }),
			help: { label: "Go to Budget", group: "Navigation" },
		},
		{
			key: "2",
			handler: () => navigate({ to: "/transactions" }),
			help: { label: "Go to All Accounts", group: "Navigation" },
		},
		...(accounts?.accounts ?? []).slice(0, 7).map((account, i) => ({
			key: String(i + 3),
			handler: () =>
				navigate({
					to: "/accounts/$accountId",
					params: { accountId: String(account.id) },
				}),
			help: { label: `Go to ${account.name}`, group: "Navigation" },
		})),
		{
			key: "h",
			handler: () => goMonth(addMonths(month, -1)),
			help: { label: "Previous month", group: "Month" },
		},
		{
			key: "l",
			handler: () => goMonth(addMonths(month, 1)),
			help: { label: "Next month", group: "Month" },
		},
		{ key: "arrowleft", handler: () => goMonth(addMonths(month, -1)) },
		{ key: "arrowright", handler: () => goMonth(addMonths(month, 1)) },
		{
			key: "t",
			handler: () => goMonth(todayMonth()),
			help: { label: "Current month", group: "Month" },
		},
		{
			key: "mod+k",
			handler: onTogglePalette,
			help: { label: "Command palette", group: "Global" },
		},
		{
			key: "?",
			handler: onToggleHelp,
			help: { label: "Keyboard shortcuts", group: "Global" },
		},
	]);

	return null;
}
