DROP INDEX IF EXISTS messages_expirable_idx;

ALTER TABLE messages DROP CONSTRAINT IF EXISTS messages_completed_drops_body;

-- The bodies completion dropped are unrecoverable; an empty one stands
-- in, which is all the restored shape can hold.
UPDATE messages SET body = '' WHERE body IS NULL;

ALTER TABLE messages ALTER COLUMN body SET NOT NULL;
