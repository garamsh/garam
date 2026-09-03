-- The one-time token an operator enrols with (ADR-0064). Registering an
-- operator mints one; the operator presents it with a certificate
-- signing request and garam signs the key the operator generated in its
-- own cluster. Nothing here is a credential to the operator's identity:
-- a row is permission to ask for one certificate, once.
--
-- Only the hash is stored, as `sessions` and `oauth_states` store
-- theirs. A row read out of a dump enrols nothing, which is the whole
-- property the design is for: garam holds no value anybody can present.
--
-- The foreign key is what answers what becomes of an unspent token when
-- the registration goes. `operators` (000017) carries no delete today
-- (ADR-0045), so the cascade decides the question before the route that
-- would raise it exists, rather than leaving a token behind for a GRN
-- the organization no longer holds. That is the opposite of
-- `operator_certificates` (000019), which deliberately has no such key:
-- a certificate already issued outlives the record, and a token that
-- has not been spent grants nothing once the record is gone.
CREATE TABLE operator_enrollment_tokens (
    token_hash   text PRIMARY KEY,
    operator_grn text NOT NULL REFERENCES operators (grn) ON DELETE CASCADE,
    minted_by    text NOT NULL,
    minted_at    timestamptz NOT NULL,
    expires_at   timestamptz NOT NULL,

    CONSTRAINT operator_enrollment_tokens_operator_service_type
        CHECK (split_part(operator_grn, ':', 4) = 'operator')
);

-- Minting sweeps the tokens that have expired, as creating an
-- invitation sweeps its own table (000013). A spent token is deleted
-- where it is spent, so what is left to sweep is the tokens nobody
-- used; this index is what the sweep reads.
CREATE INDEX operator_enrollment_tokens_expires_at_idx
    ON operator_enrollment_tokens (expires_at);
