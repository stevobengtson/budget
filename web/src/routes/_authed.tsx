import { ensureSession as ensureSessionClient } from "@better-auth-ui/react";
import { ensureSession as ensureSessionServer } from "@better-auth-ui/react/server";
import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";
import { createIsomorphicFn } from "@tanstack/react-start";
import { getRequestHeaders } from "@tanstack/react-start/server";
import { useState } from "react";
import { AppSidebar } from "@/components/app-sidebar";
import { CommandPalette } from "@/components/command-palette";
import { GlobalHotkeys } from "@/components/global-hotkeys";
import { HotkeysHelpDialog } from "@/components/hotkeys-help-dialog";
import { SiteHeader } from "@/components/site-header";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { auth } from "@/lib/auth";
import { authClient } from "@/lib/auth-client";

export const Route = createFileRoute("/_authed")({
	async beforeLoad({ context: { queryClient }, location }) {
		const ensureSession = createIsomorphicFn()
			.server(() =>
				ensureSessionServer(queryClient, auth, {
					headers: getRequestHeaders(),
				}),
			)
			.client(() => ensureSessionClient(queryClient, authClient));

		const session = await ensureSession();

		if (!session) {
			throw redirect({
				to: "/auth/$path",
				params: { path: "sign-in" },
				search: { redirectTo: location.href },
			});
		}

		return { session };
	},
	component: AuthedLayout,
});

function AuthedLayout() {
	const { session } = Route.useRouteContext();
	const [paletteOpen, setPaletteOpen] = useState(false);
	const [helpOpen, setHelpOpen] = useState(false);

	return (
		<div className="flex flex-col items-center my-auto">
			<SidebarProvider
				style={
					{
						"--header-height": "calc(var(--spacing) * 12)",
					} as React.CSSProperties
				}
			>
				<AppSidebar variant="inset" user={session.user} />
				<SidebarInset>
					<SiteHeader />
					<Outlet />
				</SidebarInset>
			</SidebarProvider>
			<GlobalHotkeys
				onTogglePalette={() => setPaletteOpen((v) => !v)}
				onToggleHelp={() => setHelpOpen((v) => !v)}
			/>
			<CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
			<HotkeysHelpDialog open={helpOpen} onOpenChange={setHelpOpen} />
		</div>
	);
}
