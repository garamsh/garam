-- ADR-0034's retention becomes representable: a completed message keeps
-- its row — sender, recipient, idempotency key, timestamps — so a retry
-- after delivery is still suppressed, and drops its body, the only
-- large field.
--
-- The dropped body is NULL rather than a sentinel: a sentinel is a
-- value a real body could carry, so nothing downstream could tell the
-- dropped state from a message that was sent that way. NULL names the
-- state, and the constraint below makes the two half-states
-- unrepresentable — an undelivered row without a body, and a completed
-- one still holding it.
ALTER TABLE messages ALTER COLUMN body DROP NOT NULL;

-- Rows completed before this migration still hold their bodies; the
-- drop applies to them too.
UPDATE messages SET body = NULL WHERE completed_at IS NOT NULL;

ALTER TABLE messages ADD CONSTRAINT messages_completed_drops_body
    CHECK ((completed_at IS NULL) = (body IS NOT NULL));

-- The sweep rides EnqueueMessage: an undelivered message older than the
-- retention window is deleted whole. Its predicate is the claimable
-- index's own partial condition, so the completed rows — the table's
-- growth — are outside what it reads, and this index makes the age
-- filter the leading column of what remains: the delete is a range scan
-- over the expired slice of the live queue, however many completed rows
-- the table holds.
CREATE INDEX messages_expirable_idx ON messages (created_at) WHERE completed_at IS NULL;
