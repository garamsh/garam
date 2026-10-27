-- name: LockTree :exec
-- Serializes every write to one organization's tree, so a position is
-- read and written by one transaction at a time.
SELECT pg_advisory_xact_lock(hashtextextended('cowork-pages/' || @org::text, 0));

-- name: ListPages :many
SELECT * FROM pages WHERE org = $1 ORDER BY parent_id NULLS FIRST, position;

-- name: GetPage :one
SELECT * FROM pages WHERE org = $1 AND id = $2;

-- name: ListSiblingIDs :many
SELECT id FROM pages
WHERE org = @org AND parent_id IS NOT DISTINCT FROM sqlc.narg(parent_id)::text
ORDER BY position, id;

-- name: CountSiblings :one
SELECT count(*) FROM pages
WHERE org = @org AND parent_id IS NOT DISTINCT FROM sqlc.narg(parent_id)::text;

-- name: InsertPage :one
INSERT INTO pages (org, id, parent_id, title, position, created_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
RETURNING *;

-- name: SetPosition :exec
UPDATE pages SET position = $3 WHERE org = $1 AND id = $2;

-- name: UpdatePage :one
UPDATE pages SET title = $3, parent_id = $4, position = $5, updated_at = $6
WHERE org = $1 AND id = $2
RETURNING *;

-- name: AncestorIDs :many
-- The page and every page above it, nearest first.
WITH RECURSIVE up (id, parent_id) AS (
    SELECT p.id, p.parent_id FROM pages p WHERE p.org = @org AND p.id = @id
    UNION ALL
    SELECT p.id, p.parent_id FROM pages p JOIN up ON p.org = @org AND p.id = up.parent_id
)
SELECT up.id FROM up;

-- name: DeletePage :execrows
DELETE FROM pages WHERE org = $1 AND id = $2;
