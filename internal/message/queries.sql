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
--
-- Handing the message out is the attempt, so the count rises here
-- rather than on a failure nobody reports: a turn that fails is
-- indistinguishable from one that never started (ADR-0018). A message
-- that has spent its attempts is not picked again.
-- name: ClaimMessage :one
UPDATE messages SET
    claim_id         = sqlc.arg('claim_id'),
    claimed_at       = now(),
    claim_expires_at = now() + sqlc.arg('lease')::interval,
    attempts         = messages.attempts + 1
WHERE id = (
    SELECT m.id FROM messages m
    WHERE m.recipient_grn = sqlc.arg('recipient_grn')
      AND m.completed_at IS NULL
      AND m.attempts < sqlc.arg('max_attempts')
      AND (m.claim_expires_at IS NULL OR m.claim_expires_at <= now())
    ORDER BY m.created_at, m.id
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- RenewClaim extends a live claim's lease, up to a ceiling measured
-- from the moment the claim was taken. The ceiling is the backstop for
-- a completion that never arrives (ADR-0018), so a renewal reaching
-- past it is held at it rather than refused: the claim then lapses on
-- schedule and the message is redelivered. A claim already lapsed,
-- handed on, or completed matches no row.
-- name: RenewClaim :one
UPDATE messages SET
    claim_expires_at = least(now() + sqlc.arg('lease')::interval, claimed_at + sqlc.arg('ceiling')::interval)
WHERE recipient_grn = sqlc.arg('recipient_grn')
  AND claim_id = sqlc.arg('claim_id')
  AND completed_at IS NULL
  AND claim_expires_at > now()
RETURNING *;

-- CompleteMessage is fenced by the claim rather than by the message's
-- state: a reader whose claim was handed on matches no row, and one
-- completing twice keeps the delivery it already reported. The
-- recipient is matched too, so the token is a fence rather than a
-- secret — a claim of another recipient's message completes nothing
-- here even when its identifier is known.
-- name: CompleteMessage :execrows
UPDATE messages SET
    completed_at = coalesce(completed_at, now())
WHERE recipient_grn = sqlc.arg('recipient_grn') AND claim_id = sqlc.arg('claim_id');
