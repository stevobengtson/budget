-- +goose Up
-- +goose StatementBegin
ALTER TABLE users ADD COLUMN pending_email TEXT;
ALTER TABLE verification_tokens DROP CONSTRAINT verification_tokens_purpose_check;
ALTER TABLE verification_tokens ADD CONSTRAINT verification_tokens_purpose_check
    CHECK (purpose IN ('email_verify','password_reset','email_change'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE verification_tokens DROP CONSTRAINT verification_tokens_purpose_check;
ALTER TABLE verification_tokens ADD CONSTRAINT verification_tokens_purpose_check
    CHECK (purpose IN ('email_verify','password_reset'));
ALTER TABLE users DROP COLUMN pending_email;
-- +goose StatementEnd
