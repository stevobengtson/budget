-- +goose Up
-- +goose StatementBegin

CREATE TABLE webauthn_credentials (
    id            BIGSERIAL PRIMARY KEY,
    user_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- The authenticator's own opaque handle. Globally unique, not per-user: the
    -- same physical key must not be registrable twice under different accounts
    -- without us noticing.
    credential_id BYTEA NOT NULL UNIQUE,
    public_key    BYTEA NOT NULL,
    -- Identifies the authenticator model. Nothing reads it yet, but it cannot
    -- be recovered after the fact, and a later library version will want it.
    aaguid        BYTEA,
    -- Monotonic counter some authenticators maintain. A value that goes
    -- BACKWARDS is the documented signal that a credential has been cloned.
    sign_count    BIGINT NOT NULL DEFAULT 0,
    -- CSV of "usb", "nfc", "internal", ... Hints the browser uses to prompt for
    -- the right thing; likewise unrecoverable later.
    transports       TEXT NOT NULL DEFAULT '',
    attestation_type TEXT NOT NULL DEFAULT '',
    -- Whether the credential may be, and currently is, synced to a cloud
    -- keychain. A passkey that is backed up survives losing the device; one
    -- that is not is a single point of failure worth telling the user about.
    backup_eligible BOOLEAN NOT NULL DEFAULT FALSE,
    backup_state    BOOLEAN NOT NULL DEFAULT FALSE,
    -- Latched once a decreasing sign_count is seen.
    clone_warning   BOOLEAN NOT NULL DEFAULT FALSE,
    -- User-supplied label, so a list of three passkeys is meaningful.
    name          TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at  TIMESTAMPTZ
);
CREATE INDEX idx_webauthn_credentials_user ON webauthn_credentials(user_id);

-- The WebAuthn user handle, stored on the authenticator forever and shown in
-- some OS credential managers.
--
-- Deliberately neither the email (it would be pinned into the authenticator and
-- survive every later address change) nor the row id (a small integer that
-- leaks how many users exist). 32 random bytes, minted on first registration.
ALTER TABLE users ADD COLUMN webauthn_handle BYTEA;
CREATE UNIQUE INDEX idx_users_webauthn_handle
    ON users(webauthn_handle) WHERE webauthn_handle IS NOT NULL;

-- Registration and sign-in both hold ceremony state between the two round
-- trips; it rides the same challenge table as the MFA flow.
ALTER TABLE auth_challenges DROP CONSTRAINT auth_challenges_kind_check;
ALTER TABLE auth_challenges ADD CONSTRAINT auth_challenges_kind_check
    CHECK (kind IN ('mfa','webauthn_login','webauthn_register'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM auth_challenges WHERE kind IN ('webauthn_login','webauthn_register');
ALTER TABLE auth_challenges DROP CONSTRAINT auth_challenges_kind_check;
ALTER TABLE auth_challenges ADD CONSTRAINT auth_challenges_kind_check
    CHECK (kind IN ('mfa'));

DROP INDEX IF EXISTS idx_users_webauthn_handle;
ALTER TABLE users DROP COLUMN webauthn_handle;

DROP TABLE webauthn_credentials;
-- +goose StatementEnd
