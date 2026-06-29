import "dotenv/config";
import { drizzle } from "drizzle-orm/node-postgres";
import * as schema from "../db/schema";

type DB = ReturnType<typeof drizzle<typeof schema>>;

let cached: DB | undefined;

/** Returns the Drizzle client, building it on first use and caching it.
 * Call this INSIDE request handlers / functions, never at module top level, so
 * the connection string is read when available (per-request on Workers).
 * NOTE: `lib/auth.ts` constructs BetterAuth at import time and calls getDb()
 * eagerly — that path resolves DATABASE_URL via the `dotenv/config` import
 * above. Finalizing request-time env wiring for Workers is a cutover-phase task. */
export function getDb(): DB {
	if (!cached) {
		const url = process.env.DATABASE_URL;
		if (!url) {
			throw new Error("DATABASE_URL is not set");
		}
		cached = drizzle(url, { schema });
	}
	return cached;
}
