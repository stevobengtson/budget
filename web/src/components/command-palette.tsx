import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
	CommandDialog,
	CommandEmpty,
	CommandGroup,
	CommandInput,
	CommandItem,
	CommandList,
} from "#/components/ui/command.tsx";
import { accountsQuery } from "#/features/accounts/queries.ts";

export function CommandPalette({
	open,
	onOpenChange,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const navigate = useNavigate();
	const { data: accounts } = useQuery(accountsQuery());

	function go(fn: () => void) {
		onOpenChange(false);
		fn();
	}

	return (
		<CommandDialog open={open} onOpenChange={onOpenChange}>
			<CommandInput placeholder="Type a command or search…" />
			<CommandList>
				<CommandEmpty>No results.</CommandEmpty>
				<CommandGroup heading="Navigation">
					<CommandItem onSelect={() => go(() => navigate({ to: "/budget" }))}>
						Budget
					</CommandItem>
					<CommandItem
						onSelect={() => go(() => navigate({ to: "/transactions" }))}
					>
						All Accounts
					</CommandItem>
					{(accounts ?? []).map((account) => (
						<CommandItem
							key={account.id}
							onSelect={() =>
								go(() =>
									navigate({
										to: "/accounts/$accountId",
										params: { accountId: account.id },
									}),
								)
							}
						>
							{account.name}
						</CommandItem>
					))}
				</CommandGroup>
			</CommandList>
		</CommandDialog>
	);
}
