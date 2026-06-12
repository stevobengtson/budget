import { viewPaths } from "@better-auth-ui/core";
import { createFileRoute, redirect } from "@tanstack/react-router";

import { Auth } from "@/components/auth/auth";

const validAuthPathSegments = new Set([...Object.values(viewPaths.auth)]);

export const Route = createFileRoute("/_public/auth/$path")({
	beforeLoad({ params: { path } }) {
		if (!validAuthPathSegments.has(path)) {
			throw redirect({ to: "/" });
		}
	},
	component: AuthPage,
});

function AuthPage() {
	const { path } = Route.useParams();

	return (
		<div className="flex min-h-svh flex-col items-center justify-center gap-6 bg-muted p-6 md:p-10">
			<div className="flex w-full max-w-sm flex-col gap-6">
				<Auth path={path} />
			</div>
		</div>
	);
}
