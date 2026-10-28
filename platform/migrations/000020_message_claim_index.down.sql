DROP INDEX IF EXISTS messages_claimable_idx;
CREATE INDEX messages_claimable_idx ON messages (recipient_grn, attempts, created_at) WHERE completed_at IS NULL;
