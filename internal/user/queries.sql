-- name: CreateUser :exec
INSERT INTO users (grn, nickname, roles, policies, organizations, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetUser :one
SELECT * FROM users WHERE grn = $1;

-- name: ListUsers :many
SELECT * FROM users
ORDER BY grn
LIMIT sqlc.narg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: UpdateUser :one
UPDATE users SET
    nickname      = coalesce(sqlc.narg('nickname'), nickname),
    roles         = coalesce(sqlc.narg('roles')::text[], roles),
    policies      = coalesce(sqlc.narg('policies')::text[], policies),
    organizations = coalesce(sqlc.narg('organizations')::text[], organizations),
    updated_at    = sqlc.arg('updated_at')
WHERE grn = sqlc.arg('grn')
RETURNING *;

-- name: DeleteUser :execrows
DELETE FROM users WHERE grn = $1;

-- name: AddUserOrganization :exec
UPDATE users SET organizations = array_append(organizations, sqlc.arg('org_grn')::text), updated_at = sqlc.arg('updated_at')
WHERE grn = sqlc.arg('user_grn') AND NOT (sqlc.arg('org_grn')::text = ANY (organizations));

-- name: RemoveUserOrganization :exec
UPDATE users SET organizations = array_remove(organizations, sqlc.arg('org_grn')::text), updated_at = sqlc.arg('updated_at')
WHERE grn = sqlc.arg('user_grn');
