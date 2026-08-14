-- +goose Up
-- +goose StatementBegin

-- auth_challenges holds sign-in state that exists between "the password was
-- right" and "a session was issued".
--
-- Deliberately NOT verification_tokens: that table's ConsumeToken marks a row
-- consumed in the same transaction as the lookup, which is right for a link
-- clicked once but wrong for a 6-digit code, which must survive several wrong
-- guesses while a counter decrements.
--
-- Also not a signed cookie: the mobile API is bearer-token-only with no cookie
-- jar, and later phases need this state on a cross-site POST (Apple's OAuth
-- form_post) where a SameSite=Lax cookie is never sent.
CREATE TABLE auth_challenges (
    id              BIGSERIAL PRIMARY KEY,
    -- Nullable from the start: a discoverable-passkey sign-in does not know who
    -- the user is until the authenticator answers. Every kind today sets it.
    user_id         BIGINT REFERENCES users(id) ON DELETE CASCADE,
    token_hash      TEXT NOT NULL UNIQUE,
    -- Widened by later phases with the 00010_email_change.sql pattern:
    -- 'webauthn_login'/'webauthn_register' (P2), 'login' (P3), 'oauth' (P4).
    kind            TEXT NOT NULL CHECK (kind IN ('mfa')),
    -- Factors this user can still satisfy, comma-separated, preferred first.
    methods         TEXT NOT NULL DEFAULT '',
    -- The emailed one-time code, hashed like every other token here. NULL until
    -- one is actually sent — a TOTP-only user never receives one.
    code_hash       TEXT,
    code_expires_at TIMESTAMPTZ,
    -- Opaque per-kind payload for later phases (WebAuthn SessionData, OIDC
    -- nonce + PKCE verifier). Sealed with crypto.Sealer when it holds a secret.
    data            BYTEA,
    attempts        INT NOT NULL DEFAULT 0,
    expires_at      TIMESTAMPTZ NOT NULL,
    consumed_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    user_agent      TEXT,
    ip              TEXT
);
CREATE INDEX idx_auth_challenges_user ON auth_challenges(user_id);
CREATE INDEX idx_auth_challenges_expires ON auth_challenges(expires_at);

-- One TOTP enrolment per user.
CREATE TABLE user_totp (
    user_id        BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    -- crypto.Sealer blob (AES-256-GCM), never the bare base32 secret.
    secret         BYTEA NOT NULL,
    -- NULL while enrolment is started but not yet proved. An unconfirmed row
    -- must never be treated as an active factor, or a user could lock themselves
    -- out by abandoning the setup screen.
    confirmed_at   TIMESTAMPTZ,
    -- The last 30-second step accepted. Without this a code stays valid for the
    -- rest of its window, so anyone who phishes one has time to replay it.
    last_used_step BIGINT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Single-use recovery codes, hashed (not encrypted) so they survive losing the
-- encryption key that TOTP secrets depend on.
CREATE TABLE recovery_codes (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash  TEXT NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (user_id, code_hash)
);
CREATE INDEX idx_recovery_codes_user ON recovery_codes(user_id);

-- Email codes are opt-in per user and independent of TOTP: they are the factor
-- available to someone with no authenticator app.
ALTER TABLE users ADD COLUMN email_otp_enabled BOOLEAN NOT NULL DEFAULT FALSE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN email_otp_enabled;
DROP TABLE recovery_codes;
DROP TABLE user_totp;
DROP TABLE auth_challenges;
-- +goose StatementEnd
