ALTER TABLE audit_credential_events
    DROP CONSTRAINT audit_credential_events_credential,
    ADD CONSTRAINT audit_credential_events_credential CHECK (credential IN (
        'operator_certificate', 'operator_credential', 'operator_enrollment_token',
        'supervisor_certificate', 'supervisor_credential', 'agent_token'));

CREATE TABLE operator_enrollment_tokens (
    token_hash   text PRIMARY KEY,
    operator_grn text NOT NULL REFERENCES operators (grn) ON DELETE CASCADE,
    minted_by    text NOT NULL,
    minted_at    timestamptz NOT NULL,
    expires_at   timestamptz NOT NULL,

    CONSTRAINT operator_enrollment_tokens_operator_service_type
        CHECK (split_part(operator_grn, ':', 4) = 'operator'),
    CONSTRAINT operator_enrollment_tokens_operator_unique UNIQUE (operator_grn)
);

CREATE INDEX operator_enrollment_tokens_expires_at_idx
    ON operator_enrollment_tokens (expires_at);

CREATE TABLE operator_credentials (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    operator_grn    text NOT NULL REFERENCES operators (grn) ON DELETE CASCADE,
    credential_hash text NOT NULL,
    issued_by       text NOT NULL,
    issued_at       timestamptz NOT NULL,

    CONSTRAINT operator_credentials_operator_unique UNIQUE (operator_grn),
    CONSTRAINT operator_credentials_hash_unique UNIQUE (credential_hash)
);

ALTER TABLE operator_tokens
    ALTER COLUMN key_id DROP NOT NULL,
    ADD COLUMN credential_id bigint REFERENCES operator_credentials (id) ON DELETE CASCADE,
    ADD CONSTRAINT operator_tokens_one_issuance
        CHECK ((credential_id IS NULL) <> (key_id IS NULL));

CREATE INDEX operator_tokens_credential_id_idx ON operator_tokens (credential_id);
