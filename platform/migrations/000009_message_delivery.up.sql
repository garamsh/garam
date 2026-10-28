-- The queue gains what ADR-0018 says it lacks: an attempt count, so a
-- message that fails the same way every time stops being redelivered,
-- and the moment a claim was taken, which is what a renewal's ceiling is
-- measured from. Without that ceiling a holder renewing forever leaves a
-- claim neither completed nor redelivered, which is a third state and
-- the least visible of them.
ALTER TABLE messages ADD COLUMN attempts integer NOT NULL DEFAULT 0;
ALTER TABLE messages ADD COLUMN claimed_at timestamptz;

-- A claim taken before this migration has no moment to measure from.
-- Its lease stands in for one, which leaves it at or past its ceiling —
-- the state an old claim nobody renewed is already in.
UPDATE messages SET claimed_at = claim_expires_at WHERE claim_id IS NOT NULL;

-- A claim is three columns now, written by the one statement that hands
-- a message out, so a row carrying part of one is corruption rather than
-- a state a reader has to handle.
ALTER TABLE messages DROP CONSTRAINT messages_claim_is_whole;
ALTER TABLE messages ADD CONSTRAINT messages_claim_is_whole
    CHECK (num_nulls(claim_id, claim_expires_at, claimed_at) IN (0, 3));

-- An exhausted message is skipped by every later claim, so the index a
-- claim reads carries the count it filters on.
DROP INDEX messages_claimable_idx;
CREATE INDEX messages_claimable_idx ON messages (recipient_grn, attempts, created_at) WHERE completed_at IS NULL;
