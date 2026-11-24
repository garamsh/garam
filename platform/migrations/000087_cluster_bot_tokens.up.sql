-- The tokens a connected cluster's connector exchanged for the bots
-- garam declared and placed on it (ADR-0189 decision 2). Only each
-- token's hash is kept, beside what it was issued for: the record that
-- acted, the declaration that placed the bot, the scope allowed and
-- when it expires. A token goes with its record and its declaration.
CREATE TABLE cluster_bot_tokens (
    token_hash     text PRIMARY KEY,
    cluster_grn    text NOT NULL REFERENCES clusters (grn) ON DELETE CASCADE,
    declaration_id text NOT NULL REFERENCES cluster_declarations (id) ON DELETE CASCADE,
    scope          text[] NOT NULL,
    issued_at      timestamptz NOT NULL,
    expires_at     timestamptz NOT NULL,

    CONSTRAINT cluster_bot_tokens_lifetime CHECK (expires_at > issued_at)
);

CREATE INDEX cluster_bot_tokens_cluster_idx ON cluster_bot_tokens (cluster_grn, expires_at);
CREATE INDEX cluster_bot_tokens_declaration_idx ON cluster_bot_tokens (declaration_id);
