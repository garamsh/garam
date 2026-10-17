-- A message signing key's private half may be held by an OpenBao
-- Transit key instead of sealed into its row (ADR-0104). A Transit row
-- records the mount, the key and the version that sign for it, and no
-- private material; a sealed row records none of the three. Every row
-- stored before this migration is a sealed row, so none is rewritten.
ALTER TABLE message_signing_keys
    ALTER COLUMN sealed_private_key DROP NOT NULL,
    ADD COLUMN transit_mount text,
    ADD COLUMN transit_key text,
    ADD COLUMN transit_version integer,
    ADD CONSTRAINT message_signing_keys_held CHECK (
        (sealed_private_key IS NOT NULL
            AND transit_mount IS NULL AND transit_key IS NULL AND transit_version IS NULL)
        OR (sealed_private_key IS NULL
            AND transit_mount <> '' AND transit_key <> '' AND transit_version > 0)
    );
