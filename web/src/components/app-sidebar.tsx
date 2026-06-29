import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { HandCoins, Landmark, PiggyBankIcon, PlusIcon } from "lucide-react";
import type * as React from "react";
import { useState } from "react";
import { Amount } from "#/components/amount.tsx";
import {
	Sidebar,
	SidebarContent,
	SidebarFooter,
	SidebarGroup,
	SidebarGroupContent,
	SidebarGroupLabel,
	SidebarHeader,
	SidebarMenu,
	SidebarMenuButton,
	SidebarMenuItem,
	SidebarRail,
} from "#/components/ui/sidebar.tsx";
import { NewAccountDialog } from "#/features/accounts/new-account-dialog.tsx";
import { accountsQuery } from "#/features/accounts/queries.ts";
import type { SessionUser } from "#/lib/auth-client.ts";
import { UserButton } from "./auth/user/user-button.tsx";

export function AppSidebar({
	user: _user,
	...props
}: React.ComponentProps<typeof Sidebar> & { user: SessionUser }) {
	const { data: accounts } = useQuery(accountsQuery());
	const [addingAccount, setAddingAccount] = useState(false);

	return (
		<>
			<Sidebar collapsible="offcanvas" {...props}>
				<SidebarHeader>
					<SidebarMenu>
						<SidebarMenuItem>
							<SidebarMenuButton
								asChild
								className="data-[slot=sidebar-menu-button]:p-1.5!"
							>
								<Link to="/budget">
									<PiggyBankIcon className="size-5!" />
									<span className="text-base font-semibold">Pigglet</span>
								</Link>
							</SidebarMenuButton>
						</SidebarMenuItem>
					</SidebarMenu>
				</SidebarHeader>
				<SidebarContent>
					<SidebarGroup>
						<SidebarGroupLabel>General</SidebarGroupLabel>
						<SidebarGroupContent>
							<SidebarMenu>
								<SidebarMenuItem>
									<SidebarMenuButton asChild tooltip="Budget">
										<Link to="/budget" activeProps={{ "data-active": true }}>
											<HandCoins />
											<span>Budget</span>
										</Link>
									</SidebarMenuButton>
								</SidebarMenuItem>
							</SidebarMenu>
						</SidebarGroupContent>
					</SidebarGroup>
					<SidebarGroup>
						<SidebarGroupLabel>Accounts</SidebarGroupLabel>
						<SidebarGroupContent>
							<SidebarMenu>
								<SidebarMenuItem>
									<SidebarMenuButton asChild tooltip="All Accounts">
										<Link
											to="/transactions"
											activeProps={{ "data-active": true }}
										>
											<Landmark />
											<span>All Accounts</span>
										</Link>
									</SidebarMenuButton>
								</SidebarMenuItem>
								{(accounts?.accounts ?? []).map((account) => (
									<SidebarMenuItem key={account.id}>
										<SidebarMenuButton asChild tooltip={account.name}>
											<Link
												to="/accounts/$accountId"
												params={{ accountId: String(account.id) }}
												activeProps={{ "data-active": true }}
												className="justify-between"
											>
												<span className="truncate">{account.name}</span>
												<Amount
													cents={account.balanceCents}
													className="text-xs"
												/>
											</Link>
										</SidebarMenuButton>
									</SidebarMenuItem>
								))}
								<SidebarMenuItem>
									<SidebarMenuButton onClick={() => setAddingAccount(true)}>
										<PlusIcon />
										<span>Add Account</span>
									</SidebarMenuButton>
								</SidebarMenuItem>
							</SidebarMenu>
						</SidebarGroupContent>
					</SidebarGroup>
				</SidebarContent>
				<SidebarFooter>
					<UserButton />
				</SidebarFooter>
				<SidebarRail />
			</Sidebar>
			<NewAccountDialog open={addingAccount} onOpenChange={setAddingAccount} />
		</>
	);
}
