-- name: CreateOperator :exec
INSERT INTO operators (grn, org_grn, registered_by, created_at)
VALUES ($1, $2, $3, $4);

-- name: GetOperator :one
SELECT * FROM operators WHERE grn = $1;

-- name: ListOperatorsByOrg :many
SELECT * FROM operators
WHERE org_grn = $1
ORDER BY grn;
