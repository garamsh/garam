-- DeleteExpiredMessages is the sweep (ADR-0034): an undelivered
-- message older than the window is deleted whole, its row no longer
-- able to suppress a retry — which the window is sized to outlast.
-- Postgres measures the window, as it measures the lease. Storing a
-- message is the queue's one write, so the sweep rides it rather than a
-- clock of the process's own, the shape 000013 set for the other tables
-- with an expiry.
-- name: DeleteExpiredMessages :exec
DELETE FROM messages
WHERE completed_at IS NULL
  AND created_at <= now() - sqlc.arg('retention')::interval;

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
--
-- Completing drops the body (ADR-0034): the row stays so a retry after
-- delivery is still suppressed, and the body, the only large field,
-- goes.
-- name: CompleteMessage :execrows
UPDATE messages SET
    completed_at = coalesce(completed_at, now()),
    body         = NULL
WHERE recipient_grn = sqlc.arg('recipient_grn') AND claim_id = sqlc.arg('claim_id');

-- GrantConsent stands the granting organization's half of the pair's
-- row, creating the row when this is the first half either side
-- grants. The pair is stored ordered, which the least/greatest pair
-- enforces on every write path; which half moves is decided by where
-- the granter sorts, not by the caller. Re-granting a half that already
-- stands only re-stamps it, so a grant is safe to repeat (ADR-0035).
-- name: GrantConsent :one
INSERT INTO message_consents (org_a, org_b, a_consented_at, b_consented_at)
VALUES (least(sqlc.arg('org'), sqlc.arg('other')), greatest(sqlc.arg('org'), sqlc.arg('other')),
        CASE WHEN sqlc.arg('org') < sqlc.arg('other') THEN now() END,
        CASE WHEN sqlc.arg('org') > sqlc.arg('other') THEN now() END)
ON CONFLICT (org_a, org_b) DO UPDATE SET
    a_consented_at = coalesce(excluded.a_consented_at, message_consents.a_consented_at),
    b_consented_at = coalesce(excluded.b_consented_at, message_consents.b_consented_at)
RETURNING *;

-- WithdrawConsent clears the withdrawing organization's own half and
-- leaves the other's standing: the relation falls, and the remaining
-- half is what a later re-grant joins. Clearing is idempotent — a half
-- already down stays down, and the end state is the one the caller
-- asked for either way.
-- name: WithdrawConsent :exec
UPDATE message_consents SET
    a_consented_at = CASE WHEN org_a = sqlc.arg('org') THEN NULL::timestamptz ELSE a_consented_at END,
    b_consented_at = CASE WHEN org_b = sqlc.arg('org') THEN NULL::timestamptz ELSE b_consented_at END
WHERE org_a = least(sqlc.arg('org'), sqlc.arg('other'))
  AND org_b = greatest(sqlc.arg('org'), sqlc.arg('other'));

-- DropEmptyConsent removes the pair's row once neither half stands:
-- the table holds live consent only, and a row with both halves down is
-- a tombstone nothing reads. It rides the withdraw, as the retention
-- sweep rides the enqueue (ADR-0034).
-- name: DropEmptyConsent :exec
DELETE FROM message_consents
WHERE org_a = least(sqlc.arg('org'), sqlc.arg('other'))
  AND org_b = greatest(sqlc.arg('org'), sqlc.arg('other'))
  AND a_consented_at IS NULL
  AND b_consented_at IS NULL;

-- ConsentStands answers the send path's one question: whether both
-- halves of the pair stand right now (ADR-0035). A missing row and a
-- half-withdrawn row are the same answer.
-- name: ConsentStands :one
SELECT EXISTS (
    SELECT 1 FROM message_consents
    WHERE org_a = least(sqlc.arg('org'), sqlc.arg('other'))
      AND org_b = greatest(sqlc.arg('org'), sqlc.arg('other'))
      AND a_consented_at IS NOT NULL
      AND b_consented_at IS NOT NULL
) AS stands;

-- ListConsents reads every relation the organization is a side of,
-- standing or half-withdrawn, so an administrator sees what its half
-- joins and what still waits on the other side.
-- name: ListConsents :many
SELECT * FROM message_consents
WHERE org_a = sqlc.arg('org') OR org_b = sqlc.arg('org')
ORDER BY org_a, org_b;
