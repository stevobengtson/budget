import { auth } from "@/lib/auth";

// Backend-For-Frontend bridge to the Go API.
//
// The browser only ever talks to this TanStack Start server (cookie session).
// Here, server-side, we mint a short-lived BetterAuth JWT for the current
// session and forward it to the Go service as a Bearer token. The Go API
// verifies it against BetterAuth's JWKS — no shared session store.
const GO_API_URL = process.env.GO_API_URL ?? "http://localhost:8080";

/**
 * Obtain a signed JWT for the current session. BetterAuth's jwt() plugin
 * returns it in the `set-auth-jwt` response header of a getSession call.
 * Throws 401 if there is no authenticated session.
 */
async function getApiToken(headers: Headers): Promise<string> {
  const res = await auth.api.getSession({ headers, asResponse: true });
  const token = res.headers.get("set-auth-jwt");
  if (!token) {
    throw new Response("Unauthorized", { status: 401 });
  }
  return token;
}

/**
 * Call the Go API on behalf of the current session. `path` is the API path
 * (e.g. "/api/v1/accounts"). Returns parsed JSON; throws a Response carrying
 * the upstream status on non-2xx so TanStack surfaces it correctly.
 */
export async function goApiFetch<T>(
  path: string,
  headers: Headers,
  init: RequestInit = {},
): Promise<T> {
  const token = await getApiToken(headers);

  const res = await fetch(`${GO_API_URL}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...init.headers,
      Authorization: `Bearer ${token}`,
    },
  });

  if (!res.ok) {
    const body = await res.text();
    throw new Response(body || res.statusText, { status: res.status });
  }

  return (await res.json()) as T;
}
