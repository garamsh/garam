-- Refuses while a Transit row exists: restoring NOT NULL fails on it,
-- and nothing here deletes a key to make it pass.
ALTER TABLE message_signing_keys
    DROP CONSTRAINT IF EXISTS message_signing_keys_held,
    ALTER COLUMN sealed_private_key SET NOT NULL,
    DROP COLUMN IF EXISTS transit_version,
    DROP COLUMN IF EXISTS transit_key,
    DROP COLUMN IF EXISTS transit_mount;
