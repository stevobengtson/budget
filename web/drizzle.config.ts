import 'dotenv/config';
import { defineConfig } from 'drizzle-kit';

export default defineConfig({
  out: './drizzle',
  schema: './src/db/schema/*',
  dialect: 'postgresql',
  dbCredentials: {
    // Migrations need a session-mode (direct/unpooled) connection. Neon's
    // transaction pooler (-pooler host) hangs drizzle-kit migrate, so prefer
    // DIRECT_DATABASE_URL when set and fall back to DATABASE_URL.
    url: (process.env.DIRECT_DATABASE_URL ?? process.env.DATABASE_URL) as string
  },
});

