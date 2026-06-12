import { createFileRoute, Link } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";

export const Route = createFileRoute("/_public/")({ component: Home });

function Home() {
	return (
		<div className="min-h-screen">
			<main className="mx-auto max-w-5xl px-6 py-8">
				<section className="mx-auto max-w-5xl space-y-6 px-6 py-20 text-center sm:py-28">
					<h1 className="text-4xl font-semibold tracking-tight text-balance sm:text-6xl">
						Budget <span className="text-primary">simply.</span>
					</h1>
					<p className="mx-auto max-w-2xl text-lg text-muted-foreground text-balance sm:text-xl">
						A simple but useful budgetting tool.
					</p>
					<div className="flex items-center justify-center gap-3 pt-2">
						<Button variant="outline">
							<Link to="/auth/$path" params={{ path: "sign-in" }}>
								Sign In
							</Link>
						</Button>
						<Button>
							<Link to="/auth/$path" params={{ path: "sign-up" }}>
								Sign Up
							</Link>
						</Button>
					</div>
					<p className="text-xs text-muted-foreground">
						No credit card required to get started.
					</p>
				</section>
			</main>
		</div>
	);
}
