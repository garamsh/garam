-- Every token garam issues to a bot's process is bound to that
-- process's key by DPoP (ADR-0191, #2078). A bot token keeps the
-- thumbprint of the key it is bound to; the proofs garam accepted
-- and the nonces it issued are kept where every replica reads them.

-- A bot token minted before this binding is a bearer token, which
-- nothing accepts any longer, so it goes rather than being left
-- unbound. A connector exchanges again, with a proof.
DELETE FROM bot_tokens;
ALTER TABLE bot_tokens ADD COLUMN jkt text NOT NULL;

-- Each (key, jti) a proof spent, kept for twice the iat window. The
-- primary key is the replay refusal, whichever replica runs the two.
CREATE TABLE dpop_jtis (
    jkt        text NOT NULL,
    jti        text NOT NULL,
    expires_at timestamptz NOT NULL,

    PRIMARY KEY (jkt, jti)
);

CREATE INDEX dpop_jtis_expires_at_idx ON dpop_jtis (expires_at);

-- Each nonce the token endpoint issued, accepted until it expires by
-- every replica.
CREATE TABLE dpop_nonces (
    nonce      text PRIMARY KEY,
    expires_at timestamptz NOT NULL
);

CREATE INDEX dpop_nonces_expires_at_idx ON dpop_nonces (expires_at);
