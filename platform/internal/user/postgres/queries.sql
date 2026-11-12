-- name: CreateUser :exec
INSERT INTO users (grn, nickname, roles, policies, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetUser :one
SELECT * FROM users WHERE grn = $1;

-- name: ListUsersByGRNs :many
SELECT * FROM users
WHERE grn = ANY(sqlc.arg('grns')::text[])
ORDER BY grn;

-- SearchUsersByGRNs answers the users among grns whose nickname
-- matches pattern or whose GRN matches id_pattern, case aside, ordered
-- by GRN. id_pattern is a LIKE pattern over the whole GRN that lib/grn
-- builds.
-- name: SearchUsersByGRNs :many
SELECT * FROM users
WHERE grn = ANY(sqlc.arg('grns')::text[])
  AND (nickname ILIKE sqlc.arg('pattern') OR grn ILIKE sqlc.arg('id_pattern'))
ORDER BY grn;

-- name: UpdateUser :one
UPDATE users SET
    nickname   = coalesce(sqlc.narg('nickname'), nickname),
    roles      = coalesce(sqlc.narg('roles')::text[], roles),
    policies   = coalesce(sqlc.narg('policies')::text[], policies),
    updated_at = sqlc.arg('updated_at')
WHERE grn = sqlc.arg('grn')
RETURNING *;

-- name: DeleteUser :execrows
DELETE FROM users WHERE grn = $1;
