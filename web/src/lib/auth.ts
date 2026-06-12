import { passkey } from "@better-auth/passkey";
import { betterAuth } from "better-auth";
import { drizzleAdapter } from "better-auth/adapters/drizzle";
import { jwt } from "better-auth/plugins";
import { tanstackStartCookies } from "better-auth/tanstack-start";
import { db } from "@/lib/db";

export const auth = betterAuth({
  user: {
    deleteUser: {
      enabled: true,
    },
  },
  advanced: {
    database: {
      // Generate canonical UUIDs for all id fields (defaults to a random
      // ~32-char string). Pairs with the uuid() columns in auth-schema.ts.
      generateId: "uuid",
    },
  },
  database: drizzleAdapter(db, {
    provider: "pg",
  }),
  emailAndPassword: {
    enabled: true,
  },
  plugins: [
    passkey(),
    // Issues short-lived JWTs (default EdDSA/Ed25519) and serves the public
    // key set at /api/auth/jwks. The Go API verifies tokens against that JWKS
    // — no shared session store, no callback. `sub` defaults to user.id (uuid).
    jwt({
      jwt: {
        issuer: process.env.BETTER_AUTH_URL,
        audience: "budget-go-api",
        expirationTime: "15m",
      },
    }),
    // Cookie integration MUST be last so it forwards Set-Cookie headers from
    // plugins whose `hooks.after` run before it (per Better Auth's warning).
    tanstackStartCookies(),
  ],
});
