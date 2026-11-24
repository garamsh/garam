-- name: CreateCluster :one
INSERT INTO clusters (grn, org_grn, created_by, created_at, name, description)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetCluster :one
SELECT * FROM clusters WHERE grn = $1;

-- name: ListClustersByOrg :many
SELECT * FROM clusters WHERE org_grn = $1 ORDER BY grn;

-- An absent argument keeps the column it would have written, so a
-- caller changing one of the two leaves the other as it stands.
-- name: UpdateClusterDetails :one
UPDATE clusters
SET name        = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description)
WHERE grn = sqlc.arg('grn')
RETURNING *;

-- name: DeleteCluster :execrows
DELETE FROM clusters WHERE grn = $1;
