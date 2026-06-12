import { createFileRoute } from "@tanstack/react-router";
import { listAccounts } from "#/server/accounts";

// Accounts now come from the Go API: the loader invokes a BFF server function
// (listAccounts) which forwards a JWT to the Go service and returns this user's
// accounts.
export const Route = createFileRoute("/_authed/accounts")({
  component: AccountsIndexPage,
  loader: async () => listAccounts(),
});

function formatCents(cents: number): string {
  return new Intl.NumberFormat(undefined, {
    style: "currency",
    currency: "USD",
  }).format(cents / 100);
}

function AccountsIndexPage() {
  const { accounts } = Route.useLoaderData();

  return (
    <div className="flex flex-1 flex-col">
      <div className="@container/main flex flex-1 flex-col gap-2">
        <div className="flex flex-col gap-4 py-4 md:gap-6 md:py-6">
          <h1 className="text-xl font-semibold">Accounts</h1>
          {accounts.length === 0 ? (
            <p className="text-muted-foreground">No accounts yet.</p>
          ) : (
            <ul className="divide-y">
              {accounts.map((a) => (
                <li
                  key={a.id}
                  className="flex items-center justify-between py-2"
                >
                  <span>{a.name}</span>
                  <span className="tabular-nums">
                    {formatCents(a.balanceCents)}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </div>
  );
}
