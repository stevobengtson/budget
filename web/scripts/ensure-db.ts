import "dotenv/config";
import { Client } from "pg";

/**
 * Creates the database named in DATABASE_URL if it does not already exist.
 *
 * Postgres has no `CREATE DATABASE IF NOT EXISTS`, so we connect to the
 * `postgres` maintenance database, check pg_database, and create on demand.
 * Safe to run repeatedly (idempotent) — used as a pre-step before db:push.
 */
async function ensureDatabase(): Promise<void> {
  const databaseUrl = process.env.DATABASE_URL;
  if (!databaseUrl) {
    throw new Error("DATABASE_URL is not set");
  }

  const url = new URL(databaseUrl);
  const dbName = decodeURIComponent(url.pathname.replace(/^\//, ""));
  if (!dbName) {
    throw new Error(`Could not parse a database name from DATABASE_URL: ${url.pathname}`);
  }

  // Reconnect to the maintenance database; CREATE DATABASE can't run while
  // connected to the target.
  url.pathname = "/postgres";
  const client = new Client({ connectionString: url.toString() });

  try {
    await client.connect();
    const { rowCount } = await client.query(
      "SELECT 1 FROM pg_database WHERE datname = $1",
      [dbName],
    );

    if (rowCount) {
      console.log(`Database "${dbName}" already exists.`);
      return;
    }

    // Database names can't be parameterized — quote the identifier to block
    // SQL injection via a crafted DATABASE_URL.
    const quoted = `"${dbName.replace(/"/g, '""')}"`;
    await client.query(`CREATE DATABASE ${quoted}`);
    console.log(`Created database "${dbName}".`);
  } finally {
    await client.end();
  }
}

ensureDatabase().catch((error) => {
  console.error("Failed to ensure database:", error instanceof Error ? error.message : error);
  process.exit(1);
});
