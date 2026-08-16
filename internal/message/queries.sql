-- EnqueueMessage stores the message, unless the sender already used the
-- key: the conflicting row is returned unchanged, because garam owns
-- duplicate suppression (ADR-0013). The update assigns the key to
-- itself, which is what makes RETURNING yield that row.
-- name: EnqueueMessage :one
INSERT INTO messages (id, recipient_grn, sender_grn, idempotency_key, body, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (sender_grn, idempotency_key) DO UPDATE SET
    idempotency_key = messages.idempotency_key
RETURNING *;

-- ClaimMessage hands one message to one reader: the row it picks is
-- locked for the statement, and a reader that meets a locked row skips
-- to the next rather than waiting behind it. The lease is measured by
-- Postgres, so no instance's clock decides when a claim lapses.
-- name: ClaimMessage :one
UPDATE messages SET
    claim_id         = sqlc.arg('claim_id'),
    claim_expires_at = now() + sqlc.arg('lease')::interval
WHERE id = (
    SELECT m.id FROM messages m
    WHERE m.recipient_grn = sqlc.arg('recipient_grn')
      AND m.completed_at IS NULL
      AND (m.claim_expires_at IS NULL OR m.claim_expires_at <= now())
    ORDER BY m.created_at, m.id
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- CompleteMessage is fenced by the claim rather than by the message's
-- state: a reader whose claim was handed on matches no row, and one
-- completing twice keeps the delivery it already reported.
-- name: CompleteMessage :execrows
UPDATE messages SET
    completed_at = coalesce(completed_at, now())
WHERE id = sqlc.arg('id') AND claim_id = sqlc.arg('claim_id');
