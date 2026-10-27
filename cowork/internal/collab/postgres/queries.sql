-- name: GetSnapshot :one
SELECT snapshot, through FROM page_snapshots WHERE org = $1 AND page_id = $2;

-- name: ListUpdatesAfter :many
SELECT seq, body FROM page_updates
WHERE org = $1 AND page_id = $2 AND seq > $3
ORDER BY seq;

-- name: AppendUpdate :exec
INSERT INTO page_updates (org, page_id, body) VALUES ($1, $2, $3);

-- name: CountUpdatesAfterSnapshot :one
SELECT count(*) FROM page_updates u
WHERE u.org = @org AND u.page_id = @page_id
  AND u.seq > coalesce((SELECT s.through FROM page_snapshots s WHERE s.org = @org AND s.page_id = @page_id), 0);

-- name: UpsertSnapshot :execrows
-- Replaces the snapshot only with a later one, so a compaction that
-- finished second with fewer updates folded in changes nothing.
INSERT INTO page_snapshots (org, page_id, snapshot, through) VALUES ($1, $2, $3, $4)
ON CONFLICT (org, page_id) DO UPDATE SET snapshot = EXCLUDED.snapshot, through = EXCLUDED.through
WHERE page_snapshots.through < EXCLUDED.through;

-- name: DeleteUpdatesThrough :exec
DELETE FROM page_updates WHERE org = $1 AND page_id = $2 AND seq <= $3;
