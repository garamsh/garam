-- garam signs every message it hands out (ADR-0101), so what it signs
-- as the author has to be stored with the message. A sender authors
-- every message but one kind: the cross-organization refusal garam
-- writes under the intended recipient's identity (ADR-0035), which is
-- signed as garam's own. Every row stored before this migration was
-- posted by its sender, so the default is what each already was.
ALTER TABLE messages
    ADD COLUMN author text NOT NULL DEFAULT 'sender',
    ADD CONSTRAINT messages_author CHECK (author IN ('sender', 'garam'));

-- The keys garam signs messages with (ADR-0101). The private half is
-- sealed under the key-encryption key, as a certificate authority's is
-- (ADR-0016), and bound to its own row. A key signs from signs_from
-- until a later key's signs_from passes; withdrawn_at removes it from
-- both signing and publication at once. No row is ever deleted, so a
-- key that stopped signing can never become the newest again.
CREATE TABLE message_signing_keys (
    -- The key's JWK thumbprint (RFC 7638), which is its kid.
    id                 text PRIMARY KEY,
    -- The public half, as an uncompressed P-256 point (SEC 1).
    public_key         bytea NOT NULL,
    sealed_private_key bytea NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    signs_from         timestamptz NOT NULL,
    withdrawn_at       timestamptz
);
