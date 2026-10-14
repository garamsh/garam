-- The public keys an operator proves its registration with, the
-- assertion identifiers it has spent, and which key a token was
-- exchanged under (ADR-0102).
--
-- A key row holds the verification public JWK and never the private
-- key, which garam answers once and keeps nowhere. A row outlives its
-- withdrawal: revoked_at records it, so an organization can read back
-- which keys its operator held. A planned rotation sets the
-- predecessor's retires_at, after which it reads as withdrawn without
-- a write. last_exchanged_at is the cohort evidence an inventory reads
-- for an idle operator, which no request metric shows. A key cascades
-- from the registration, so deleting the operator takes every key, and
-- a reused operator_id starts with none.
CREATE TABLE operator_keys (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    operator_grn      text NOT NULL REFERENCES operators (grn) ON DELETE CASCADE,
    kid               text NOT NULL,
    algorithm         text NOT NULL,
    public_jwk        text NOT NULL,
    issued_by         text NOT NULL,
    issued_at         timestamptz NOT NULL,
    retires_at        timestamptz,
    revoked_at        timestamptz,
    last_exchanged_at timestamptz,

    CONSTRAINT operator_keys_kid_unique UNIQUE (operator_grn, kid),
    CONSTRAINT operator_keys_algorithm CHECK (algorithm = 'RS256')
);

CREATE INDEX operator_keys_operator_grn_idx ON operator_keys (operator_grn);

-- Each (client, jti) an exchange admitted, kept until the assertion's
-- exp plus the 30 seconds of skew. The primary key is the replay
-- refusal: two exchanges of one assertion conflict on it whichever
-- replica runs them. It references no registration, so deleting and
-- re-registering an operator clears nothing.
CREATE TABLE operator_assertions (
    operator_grn text NOT NULL,
    jti          text NOT NULL,
    expires_at   timestamptz NOT NULL,

    PRIMARY KEY (operator_grn, jti)
);

CREATE INDEX operator_assertions_operator_grn_expires_at_idx ON operator_assertions (operator_grn, expires_at);

-- A token rests on exactly one issuance: the shared-secret credential
-- of machine-token.v1 or the key of machine-token.v2, which is also
-- what tells the two cohorts apart. Withdrawing a key deletes its
-- tokens, and deleting the key row would too.
ALTER TABLE operator_tokens
    ALTER COLUMN credential_id DROP NOT NULL,
    ADD COLUMN key_id bigint REFERENCES operator_keys (id) ON DELETE CASCADE,
    ADD CONSTRAINT operator_tokens_one_issuance
        CHECK ((credential_id IS NULL) <> (key_id IS NULL));

CREATE INDEX operator_tokens_key_id_idx ON operator_tokens (key_id);
