-- What a connected cluster authenticates with (ADR-0184 decisions 3 to
-- 7): the hash of its one-time registration token, the public keys
-- its connector generated, the tokens those keys were exchanged for,
-- and the jtis its signed requests spent. No private key, Secret value
-- or token value is stored (decision 8).

-- One registration token per cluster at a time: minting another
-- replaces it. The row is deleted when the token is presented,
-- whatever the outcome, and an expired one is swept.
CREATE TABLE cluster_registration_tokens (
    cluster_grn text PRIMARY KEY REFERENCES clusters (grn) ON DELETE CASCADE,
    token_hash  text NOT NULL UNIQUE,
    expires_at  timestamptz NOT NULL
);

CREATE INDEX cluster_registration_tokens_expires_at_idx ON cluster_registration_tokens (expires_at);

-- A key's state is derived from the row and the database's time:
-- revoked where revoked_at is set or retires_at has passed, retiring
-- where retires_at is ahead, active otherwise. A key once held stays,
-- withdrawn, so a rotation to it is refused (ROT-4). predecessor_id is
-- the key a rotation made this one succeed, and NULL for the key a
-- registration bound: a rotation repeated to a live successor is
-- answered with its predecessor's deadline, and to any other key held
-- is refused (ROT-4).
CREATE TABLE cluster_keys (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    cluster_grn    text NOT NULL REFERENCES clusters (grn) ON DELETE CASCADE,
    kid            text NOT NULL,
    public_jwk     text NOT NULL,
    created_at     timestamptz NOT NULL,
    retires_at     timestamptz,
    revoked_at     timestamptz,
    predecessor_id bigint REFERENCES cluster_keys (id) ON DELETE CASCADE,

    CONSTRAINT cluster_keys_kid_unique UNIQUE (cluster_grn, kid)
);

-- Withdrawing a key deletes its tokens, and deleting the key or the
-- cluster does too.
CREATE TABLE cluster_tokens (
    token_hash  text PRIMARY KEY,
    cluster_grn text NOT NULL REFERENCES clusters (grn) ON DELETE CASCADE,
    key_id      bigint NOT NULL REFERENCES cluster_keys (id) ON DELETE CASCADE,
    issued_at   timestamptz NOT NULL,
    expires_at  timestamptz NOT NULL
);

CREATE INDEX cluster_tokens_key_id_idx ON cluster_tokens (key_id);
CREATE INDEX cluster_tokens_cluster_grn_expires_at_idx ON cluster_tokens (cluster_grn, expires_at);

-- Each (cluster, jti) a signed request spent, an exchange's assertion
-- or a rotation's two JWSs, kept until its exp plus the skew. The
-- primary key is the replay refusal, whichever replica runs the two.
-- It references no cluster, so re-registering clears nothing.
CREATE TABLE cluster_jtis (
    cluster_grn text NOT NULL,
    jti         text NOT NULL,
    expires_at  timestamptz NOT NULL,

    PRIMARY KEY (cluster_grn, jti)
);

CREATE INDEX cluster_jtis_expires_at_idx ON cluster_jtis (expires_at);
