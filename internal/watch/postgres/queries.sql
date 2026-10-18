-- OpenWatch records a live watch. The lease travels as an interval, so
-- Postgres's clock decides when the watch lapses and no instance's own
-- does.
-- name: OpenWatch :exec
INSERT INTO agent_watches (id, agent_grn, expires_at)
VALUES (sqlc.arg('id'), sqlc.arg('agent_grn'), now() + sqlc.arg('lease')::interval);

-- RefreshWatch carries a live watch's lease forward. A watch that has
-- lapsed or been closed matches no row, so its holder learns the stream
-- it is serving is no longer counted instead of holding one open that
-- nothing forwards to.
-- name: RefreshWatch :execrows
UPDATE agent_watches SET expires_at = now() + sqlc.arg('lease')::interval
WHERE id = sqlc.arg('id') AND expires_at > now();

-- CloseWatch ends the watch. A watch already lapsed matches no row and
-- is already not counted, which is the same end.
-- name: CloseWatch :exec
DELETE FROM agent_watches WHERE id = sqlc.arg('id');

-- CountLiveWatches is what a poll response carries. It counts the
-- watches on one agent rather than reading a row about the agent: there
-- is none, and an agent nobody watches counts zero from an empty table.
-- name: CountLiveWatches :one
SELECT count(*) FROM agent_watches
WHERE agent_grn = sqlc.arg('agent_grn') AND expires_at > now();

-- DeleteLapsedWatches drops the watches nobody closed, which is what a
-- process that died holding a stream leaves behind.
-- name: DeleteLapsedWatches :exec
DELETE FROM agent_watches WHERE expires_at <= now();

-- AdmitExecution runs the agent execution fence (ADR-0080) as the
-- first statement of a protected write's transaction; the row it locks
-- is held until that transaction ends.
-- name: AdmitExecution :one
SELECT agent_execution_admit(
    sqlc.arg('agent_grn'), sqlc.narg('token_hash')::bytea, sqlc.arg('serial'),
    sqlc.arg('not_before')::timestamptz, sqlc.arg('not_after')::timestamptz
)::text;
