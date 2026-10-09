DROP TABLE IF EXISTS message_signing_keys;
ALTER TABLE messages
    DROP CONSTRAINT IF EXISTS messages_author,
    DROP COLUMN IF EXISTS author;
