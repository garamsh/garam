-- Every token garam issues to a bot's process is bound to that
-- process's key by DPoP (ADR-0191, #2078). A bot token keeps the
-- thumbprint of the key it is bound to; the proofs garam accepted
-- and the nonces it issued are kept where every replica reads them.

-- A bot token minted before this binding is a bearer token, which
-- nothing accepts any longer, so it goes rather than being left
-- unbound. A connector exchanges again, with a proof.
DELETE FROM bot_tokens;
ALTER TABLE bot_tokens ADD COLUMN jkt text NOT NULL
    CONSTRAINT bot_tokens_jkt_bound CHECK (jkt <> '');

-- Each (key, jti) a proof spent, kept for twice the iat window. The
-- primary key is the replay refusal, whichever replica runs the two.
CREATE TABLE dpop_jtis (
    jkt        text NOT NULL,
    jti        text NOT NULL,
    expires_at timestamptz NOT NULL,

    PRIMARY KEY (jkt, jti)
);

CREATE INDEX dpop_jtis_expires_at_idx ON dpop_jtis (expires_at);

-- One nonce per period of the token endpoint's, numbered from the
-- epoch: the first replica answering in a period writes it, every
-- replica answers and accepts it until the next period ends, and the
-- rest are swept, so a flood of unauthenticated requests adds no row.
CREATE TABLE dpop_nonces (
    period bigint PRIMARY KEY,
    nonce  text NOT NULL UNIQUE
);
