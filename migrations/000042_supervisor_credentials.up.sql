-- A standalone host's supervisor under standalone-enrollment.v2
-- (ADR-0100 G1d): spending its approval answers a credential rather
-- than signing two certificate requests, and the supervisor exchanges
-- that credential for hour-long tokens.
--
-- An approval binds the host's two key fingerprints, or neither. Which
-- it holds decides the contract it is spent under — both for v1, none
-- for v2 — so neither contract can spend the other's approval. A v2
-- spending records the agent's placement and no certificate.
ALTER TABLE agent_enrollments
    ALTER COLUMN supervisor_key_sha256 DROP NOT NULL,
    ALTER COLUMN agent_key_sha256 DROP NOT NULL,
    ADD CONSTRAINT agent_enrollments_keys_whole
        CHECK ((supervisor_key_sha256 IS NULL) = (agent_key_sha256 IS NULL)),
    DROP CONSTRAINT agent_enrollments_consumption_is_whole,
    ADD CONSTRAINT agent_enrollments_consumption_is_whole
        CHECK ((consumed_at IS NULL) = (request_sha256 IS NULL)
           AND (consumed_at IS NULL) = (epoch IS NULL)
           AND (consumed_at IS NULL) = (lineage IS NULL)
           AND (consumed_at IS NOT NULL AND supervisor_key_sha256 IS NOT NULL)
               = (supervisor_certificate_pem IS NOT NULL)
           AND (supervisor_certificate_pem IS NULL) = (agent_certificate_pem IS NULL)
           AND (supervisor_certificate_pem IS NULL) = (issuer_pem IS NULL)
           AND (supervisor_certificate_pem IS NULL) = (server_root_pem IS NULL));

-- A supervisor registered under v2 holds no certificate.
ALTER TABLE agent_supervisors
    ALTER COLUMN serial DROP NOT NULL,
    ALTER COLUMN not_after DROP NOT NULL,
    ADD CONSTRAINT agent_supervisors_certificate_whole
        CHECK ((serial IS NULL) = (not_after IS NULL));

-- The credential garam answered the supervisor, stored as its hash
-- alone, and the tokens it is exchanged for, as operator_credentials
-- and operator_tokens hold an operator's (000038). A supervisor holds
-- one credential: a retried spending re-issues it, deleting the row and
-- every token minted under it. Both cascade from the registration; a
-- revocation or a replacement ends the registration rather than
-- deleting it, and every read of a credential or a token joins it to a
-- registration that has not ended.
CREATE TABLE supervisor_credentials (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    supervisor_grn  text NOT NULL REFERENCES agent_supervisors (grn) ON DELETE CASCADE,
    credential_hash text NOT NULL,
    issued_at       timestamptz NOT NULL,

    CONSTRAINT supervisor_credentials_supervisor_unique UNIQUE (supervisor_grn),
    CONSTRAINT supervisor_credentials_hash_unique UNIQUE (credential_hash)
);

CREATE TABLE supervisor_tokens (
    token_hash     text PRIMARY KEY,
    credential_id  bigint NOT NULL REFERENCES supervisor_credentials (id) ON DELETE CASCADE,
    supervisor_grn text NOT NULL REFERENCES agent_supervisors (grn) ON DELETE CASCADE,
    issued_at      timestamptz NOT NULL,
    expires_at     timestamptz NOT NULL
);

-- The trim an exchange runs reads a credential's tokens by age.
CREATE INDEX supervisor_tokens_credential_id_issued_at_idx ON supervisor_tokens (credential_id, issued_at);
