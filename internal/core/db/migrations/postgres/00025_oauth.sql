-- +goose Up
-- +goose StatementBegin

-- Federated sign-in identities.
--
-- A table rather than columns on users, for two reasons that are not
-- negotiable. Apple's identity is a per-Team `sub` that is NOT the email —
-- Hide My Email hands out a revocable relay address, so the address cannot be
-- the key. And one account must be able to hold both a Google and an Apple
-- identity at once, which columns cannot express.
CREATE TABLE oauth_identities (
    id       BIGSERIAL PRIMARY KEY,
    user_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider TEXT NOT NULL CHECK (provider IN ('google','apple')),
    -- The provider's stable subject identifier. Unique WITH the provider, not
    -- on its own: two providers may independently mint the same string.
    subject  TEXT NOT NULL,
    -- The address the provider reported. Apple sends this only on the FIRST
    -- authorization, so it is persisted then or lost for good.
    email    TEXT,
    -- Whether the provider asserted the address is verified. Never assumed:
    -- Google reports false for some Workspace configurations, and an unverified
    -- assertion must not be allowed to claim an existing account.
    email_verified BOOLEAN NOT NULL DEFAULT FALSE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at   TIMESTAMPTZ,
    UNIQUE (provider, subject)
);
CREATE INDEX idx_oauth_identities_user ON oauth_identities(user_id);

-- The OAuth handshake holds state (the `state` parameter, the nonce, and the
-- PKCE verifier) between the redirect out and the callback back.
--
-- It rides auth_challenges rather than a cookie because Apple returns via a
-- cross-site form_post, on which a SameSite=Lax cookie is simply not sent. A
-- cookie-based implementation works perfectly with Google and fails only with
-- Apple, which is a miserable thing to debug.
ALTER TABLE auth_challenges DROP CONSTRAINT auth_challenges_kind_check;
ALTER TABLE auth_challenges ADD CONSTRAINT auth_challenges_kind_check
    CHECK (kind IN ('mfa','webauthn_login','webauthn_register','login','oauth'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM auth_challenges WHERE kind = 'oauth';
ALTER TABLE auth_challenges DROP CONSTRAINT auth_challenges_kind_check;
ALTER TABLE auth_challenges ADD CONSTRAINT auth_challenges_kind_check
    CHECK (kind IN ('mfa','webauthn_login','webauthn_register','login'));

DROP TABLE oauth_identities;
-- +goose StatementEnd
