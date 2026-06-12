import { createServerFn } from "@tanstack/react-start";
import { getRequest } from "@tanstack/react-start/server";
import { goApiFetch } from "@/server/go-api";

// Account mirrors the Go API's accountDTO (camelCase, cents as integers).
export type Account = {
  id: number;
  name: string;
  type: string;
  startingBalanceCents: number;
  creditLimitCents?: number;
  aprBps?: number;
  monthlyPaymentCents?: number;
  includeInPaydown: boolean;
  paymentCategoryId?: number;
  archivedAt?: string;
  createdAt: string;
  balanceCents: number;
};

// listAccounts is a BFF server function: it runs on the TanStack server, mints
// a JWT for the current session, and fetches the user's accounts from the Go
// API. The Go API scopes results to the authenticated user.
export const listAccounts = createServerFn({ method: "GET" }).handler(
  async () => {
    const { headers } = getRequest();
    return goApiFetch<{ accounts: Account[] }>("/api/v1/accounts", headers);
  },
);
