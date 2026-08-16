-- A message addressed to an agent, held until a reader claims it and
-- reports it delivered (ADR-0013). The queue is a table rather than a
-- connection: any garam instance answers any poll, so nothing about a
-- recipient lives in a process.
--
-- recipient_grn and sender_grn carry no foreign key. garam answers the
-- sender once the message is stored and never rejects a routing failure
-- (ADR-0013), so a message to a recipient no row knows about has to be
-- storable; a sender may also be a channel garam holds no row for.
CREATE TABLE messages (
    id               text PRIMARY KEY,
    recipient_grn    text NOT NULL,
    sender_grn       text NOT NULL,
    -- The key is the sender's, so two senders may pick the same one.
    -- Re-sending it stores nothing new: duplicate suppression is
    -- garam's (ADR-0013). It outlives completion, so a retry arriving
    -- after delivery is still suppressed.
    idempotency_key  text NOT NULL,
    body             text NOT NULL,
    created_at       timestamptz NOT NULL,
    -- A claim is one reader's hold and the token that fences it: the
    -- next claim replaces the token, so a reader whose lease lapsed
    -- completes nothing. A lease that runs out leaves the message
    -- claimable rather than stranded.
    claim_id         text,
    claim_expires_at timestamptz,
    completed_at     timestamptz,
    UNIQUE (sender_grn, idempotency_key),
    CONSTRAINT messages_claim_is_whole CHECK ((claim_id IS NULL) = (claim_expires_at IS NULL)),
    CONSTRAINT messages_completed_was_claimed CHECK (completed_at IS NULL OR claim_id IS NOT NULL)
);

-- A claim reads one recipient's uncompleted messages oldest first.
-- Completed rows stay for their idempotency key alone and are indexed
-- by it, not here.
CREATE INDEX messages_claimable_idx ON messages (recipient_grn, created_at) WHERE completed_at IS NULL;
