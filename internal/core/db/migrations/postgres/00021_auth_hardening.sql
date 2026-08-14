-- +goose Up
-- +goose StatementBegin

-- Sessions gain the columns an "active sessions" UI needs, plus the step-up
-- timestamp. last_seen_at is written lazily by the app (only when it is already
-- stale by ~15 minutes) — AuthenticateSession runs on every authenticated
-- request, so an unconditional UPDATE there would turn every page view into a
-- write on this table.
ALTER TABLE sessions ADD COLUMN last_seen_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP;
-- Last time a factor was re-proved. NULL means "never since creation", and
-- created_at is the fallback, so existing sessions behave as if freshly
-- authenticated at the moment they were made.
ALTER TABLE sessions ADD COLUMN reauth_at    TIMESTAMPTZ;
-- Which surface minted this session. The mobile bearer token is a row in this
-- same table, so the sessions list must be able to say "iPhone" and not "web".
ALTER TABLE sessions ADD COLUMN client       TEXT NOT NULL DEFAULT 'web';
-- Human label derived from the user agent at creation ("Chrome on macOS"),
-- frozen at that point: the UA of the request that created the session is the
-- only one that describes the device the user is trying to recognise.
ALTER TABLE sessions ADD COLUMN label        TEXT;
CREATE INDEX idx_sessions_expires ON sessions(expires_at);

-- Per-account login lockout. Lives in Postgres rather than memory because it
-- must survive a deploy (otherwise an attacker just waits for one) and must be
-- visible to the user and the admin console.
--
-- subject is the normalized email, NOT a user_id foreign key: an attempt
-- against an address that does not exist has to be counted too, or the
-- difference in behaviour between "locked" and "never locks" enumerates which
-- addresses are registered.
CREATE TABLE auth_lockouts (
    id              BIGSERIAL PRIMARY KEY,
    subject         TEXT NOT NULL,
    scope           TEXT NOT NULL CHECK (scope IN ('password_login')),
    failures        INT NOT NULL DEFAULT 0,
    locked_until    TIMESTAMPTZ,
    last_failure_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (scope, subject)
);

-- Passkey-only and OAuth-only users arrive in later phases and have no
-- password at all. The column is relaxed here, together with the code paths
-- that tolerate an empty hash, so those phases need no further users DDL.
ALTER TABLE users ALTER COLUMN password_hash DROP NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Rolling back means re-imposing NOT NULL, so any password-less row needs a
-- value first. An empty hash fails verification cleanly rather than matching
-- anything, which is the safe direction for a rollback.
UPDATE users SET password_hash = '' WHERE password_hash IS NULL;
ALTER TABLE users ALTER COLUMN password_hash SET NOT NULL;

DROP TABLE auth_lockouts;

DROP INDEX IF EXISTS idx_sessions_expires;
ALTER TABLE sessions DROP COLUMN label;
ALTER TABLE sessions DROP COLUMN client;
ALTER TABLE sessions DROP COLUMN reauth_at;
ALTER TABLE sessions DROP COLUMN last_seen_at;
-- +goose StatementEnd
