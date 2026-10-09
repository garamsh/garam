-- The credential garam issues an operator, and the short-lived tokens
-- the operator exchanges it for (ADR-0100).
--
-- A credential is shown once, by the call that issues it, and only its
-- hash is stored, as `sessions` and `operator_enrollment_tokens` store
-- theirs: a row read out of a dump authenticates nothing. An operator
-- holds one credential. Re-issuing deletes the row and inserts its
-- successor in one transaction, so the identifier the tokens reference
-- changes and every token minted under the old credential goes with it
-- by the foreign key below rather than by a statement each re-issue has
-- to remember.
--
-- Both tables reference the registration with ON DELETE CASCADE, which
-- is what makes deleting an operator stop its tokens on the next
-- request (ADR-0070, ADR-0100): the record is what a token's every
-- request reads beside it.
CREATE TABLE operator_credentials (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    operator_grn    text NOT NULL REFERENCES operators (grn) ON DELETE CASCADE,
    credential_hash text NOT NULL,
    issued_by       text NOT NULL,
    issued_at       timestamptz NOT NULL,

    CONSTRAINT operator_credentials_operator_unique UNIQUE (operator_grn),
    CONSTRAINT operator_credentials_hash_unique UNIQUE (credential_hash)
);

-- A token names the operator it was exchanged for and the audience it
-- is accepted by: NULL is garam itself, and a GRN is a hosted
-- operator's, accepted by garam only on the introspection that hosted
-- operator calls (ADR-0100). The operator's GRN is carried beside the
-- credential so the per-request read joins the registration directly.
CREATE TABLE operator_tokens (
    token_hash    text PRIMARY KEY,
    credential_id bigint NOT NULL REFERENCES operator_credentials (id) ON DELETE CASCADE,
    operator_grn  text NOT NULL REFERENCES operators (grn) ON DELETE CASCADE,
    audience_grn  text,
    issued_at     timestamptz NOT NULL,
    expires_at    timestamptz NOT NULL,

    CONSTRAINT operator_tokens_audience_service_type
        CHECK (audience_grn IS NULL OR split_part(audience_grn, ':', 4) = 'operator')
);

-- The re-issue cascade reads the tokens by credential, and the sweep an
-- exchange runs reads them by operator and expiry.
CREATE INDEX operator_tokens_credential_id_idx ON operator_tokens (credential_id);
CREATE INDEX operator_tokens_operator_grn_expires_at_idx ON operator_tokens (operator_grn, expires_at);
