-- name: CreateUser :exec
INSERT INTO users (grn, nickname, roles, policies, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetUser :one
SELECT * FROM users WHERE grn = $1;

-- name: ListUsers :many
SELECT * FROM users
ORDER BY grn
LIMIT sqlc.narg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: ListUsersByGRNs :many
SELECT * FROM users
WHERE grn = ANY(sqlc.arg('grns')::text[])
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
