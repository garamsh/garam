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
-- itself, which is what makes RETURNING yield that row. Postgres stamps
-- created_at, so the window the sweep measures starts on its clock too.
-- name: EnqueueMessage :one
INSERT INTO messages (id, recipient_grn, sender_grn, idempotency_key, body, author, created_at)
VALUES ($1, $2, $3, $4, $5, $6, now())
ON CONFLICT (sender_grn, idempotency_key) DO UPDATE SET
    idempotency_key = messages.idempotency_key
RETURNING *;

-- MessageByKey reads what one sender's key stores: what a durable
-- submission under that key is answered with, whatever its budget now
-- says (ADR-0083).
-- name: MessageByKey :one
SELECT * FROM messages
WHERE sender_grn = sqlc.arg('sender_grn') AND idempotency_key = sqlc.arg('idempotency_key');

-- EnqueueWithinBudget stores a durable submission's message only while
-- its creation instant is inside the budget at the decision instant
-- (ADR-0083), and answers 'admitted' where a row was stored or a
-- concurrent one under the key returned, otherwise why not. The
-- decision instant is clock_timestamp() read once as this statement
-- runs, not now(), which is when the transaction began: the fence's
-- lock wait falls between the two, and a request delayed there is
-- judged when it writes. It also stamps created_at, so retention runs
-- from the decision on the clock that judged it.
-- name: EnqueueWithinBudget :one
WITH decision AS MATERIALIZED (
    SELECT clock_timestamp() AS decided_at
), stored AS (
    INSERT INTO messages (id, recipient_grn, sender_grn, idempotency_key, body, author, created_at)
    SELECT sqlc.arg('id')::text, sqlc.arg('recipient_grn')::text, sqlc.arg('sender_grn')::text,
           sqlc.arg('idempotency_key')::text, sqlc.arg('body')::text, sqlc.arg('author')::text,
           decision.decided_at
    FROM decision
    WHERE sqlc.arg('created')::timestamptz <= decision.decided_at
      AND sqlc.arg('created')::timestamptz + sqlc.arg('budget')::interval > decision.decided_at
    ON CONFLICT (sender_grn, idempotency_key) DO UPDATE SET
        idempotency_key = messages.idempotency_key
    RETURNING 1
)
SELECT CASE
    WHEN EXISTS (SELECT 1 FROM stored) THEN 'admitted'
    WHEN sqlc.arg('created')::timestamptz > decision.decided_at THEN 'later'
    ELSE 'expired'
END::text AS verdict
FROM decision;

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

-- MessageStatus reads how far one sender's message has got (ADR-0055).
-- The sender is matched beside the identifier, so the identifier alone
-- reaches nothing and a message another identity sent is answered like
-- one that is not there.
--
-- The state is decided here rather than in the process, and by the
-- expressions ClaimMessage picks a message with: whether a claim still
-- stands is `now()` against the same lease column Postgres already
-- measures, and the attempt bound is the one the claim filters on,
-- passed from the same configuration. So a message answered `pending`
-- is one that claim would pick and a message answered `exhausted` is
-- one it would skip. The body is not selected: a completed message has
-- none (ADR-0034) and a sender wrote its own.
--
-- No state means expired. The sweep deletes an expired message whole,
-- and a message past the window it has not reached yet is still handed
-- out, so absence is the answer and this query returns no row for it.
-- name: MessageStatus :one
SELECT
    id,
    attempts,
    created_at,
    CASE
        WHEN completed_at IS NOT NULL THEN 'completed'
        WHEN claim_expires_at > now() THEN 'claimed'
        WHEN attempts >= sqlc.arg('max_attempts') THEN 'exhausted'
        ELSE 'pending'
    END::text AS state
FROM messages
WHERE id = sqlc.arg('id') AND sender_grn = sqlc.arg('sender_grn');

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

-- WithdrawConsent clears the withdrawing organization's own half while
-- the other still stands: the relation falls, and the half left up is
-- what a later re-grant joins. Clearing is idempotent — a half already
-- down stays down, and the end state is the one the caller asked for
-- either way. The other half standing is the statement's condition,
-- because a row neither half stands in is not a state the table holds:
-- that case is DropEmptyConsent's.
-- name: WithdrawConsent :exec
UPDATE message_consents SET
    a_consented_at = CASE WHEN org_a = sqlc.arg('org') THEN NULL::timestamptz ELSE a_consented_at END,
    b_consented_at = CASE WHEN org_b = sqlc.arg('org') THEN NULL::timestamptz ELSE b_consented_at END
WHERE org_a = least(sqlc.arg('org'), sqlc.arg('other'))
  AND org_b = greatest(sqlc.arg('org'), sqlc.arg('other'))
  AND (CASE WHEN org_a = sqlc.arg('org') THEN b_consented_at ELSE a_consented_at END) IS NOT NULL;

-- DropEmptyConsent removes the pair's row when the withdrawing side's
-- was the last half standing: the table holds live consent only, and a
-- row with both halves down is a tombstone nothing reads. It rides the
-- withdraw, as the retention sweep rides the enqueue (ADR-0034), and it
-- is what does nothing where no row ever stood.
-- name: DropEmptyConsent :exec
DELETE FROM message_consents
WHERE org_a = least(sqlc.arg('org'), sqlc.arg('other'))
  AND org_b = greatest(sqlc.arg('org'), sqlc.arg('other'))
  AND (CASE WHEN org_a = sqlc.arg('org') THEN b_consented_at ELSE a_consented_at END) IS NULL;

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

-- name: CountConsentsByOrg :one
-- The consent relations the organization is a side of, which deleting
-- the organization removes. A relation is a pair, so each one counted
-- here is also the other organization's, and deleting this one takes
-- that side's half with it.
SELECT count(*) FROM message_consents
WHERE org_a = sqlc.arg('org') OR org_b = sqlc.arg('org');

-- AdmitExecution runs the agent execution fence (ADR-0080) as the
-- first statement of a protected write's transaction; the row it locks
-- is held until that transaction ends.
-- name: AdmitExecution :one
SELECT agent_execution_admit(
    sqlc.arg('agent_grn'), sqlc.narg('token_hash')::bytea, sqlc.arg('serial'),
    sqlc.arg('not_before')::timestamptz, sqlc.arg('not_after')::timestamptz
)::text;

-- SigningKeys reads every key garam has signed messages with or will,
-- in the order they take over signing (ADR-0101). The rows are few —
-- one per rotation — so which of them signs and which are published is
-- decided over all of them rather than in a predicate.
-- name: SigningKeys :many
SELECT * FROM message_signing_keys
ORDER BY signs_from, created_at, id;

-- DatabaseNow is the instant the keys above are judged against. It is
-- the clock when the statement runs, not now(), which is when the
-- transaction began: under the rotation lock that is before the wait,
-- so a key a replica stored while this one waited would read as not
-- yet begun, and the lead would be short by the wait.
-- name: DatabaseNow :one
SELECT clock_timestamp()::timestamptz;

-- LockSigningKeys serializes rotation across every replica for the
-- rest of the transaction, so two processes that both find a key due
-- create one between them.
-- name: LockSigningKeys :exec
SELECT pg_advisory_xact_lock(hashtext('message_signing_keys'));

-- InsertSigningKey stores a new key that signs from signs_from, which
-- the caller derives from the instant it decided the key was due.
-- name: InsertSigningKey :exec
INSERT INTO message_signing_keys (id, public_key, sealed_private_key, signs_from)
VALUES (sqlc.arg('id'), sqlc.arg('public_key'), sqlc.arg('sealed_private_key'),
        sqlc.arg('signs_from'));

-- WithdrawSigningKey stops a key signing and removes it from the
-- published set at once. Withdrawing one already withdrawn keeps the
-- instant it was first withdrawn.
-- name: WithdrawSigningKey :one
UPDATE message_signing_keys SET withdrawn_at = coalesce(withdrawn_at, now())
WHERE id = sqlc.arg('id')
RETURNING id;
