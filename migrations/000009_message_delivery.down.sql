DROP INDEX IF EXISTS messages_claimable_idx;
CREATE INDEX messages_claimable_idx ON messages (recipient_grn, created_at) WHERE completed_at IS NULL;

ALTER TABLE messages DROP CONSTRAINT IF EXISTS messages_claim_is_whole;
ALTER TABLE messages ADD CONSTRAINT messages_claim_is_whole
    CHECK ((claim_id IS NULL) = (claim_expires_at IS NULL));

ALTER TABLE messages DROP COLUMN IF EXISTS claimed_at;
ALTER TABLE messages DROP COLUMN IF EXISTS attempts;
