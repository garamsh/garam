-- name: CreateOperator :exec
INSERT INTO operators (grn, org_grn, registered_by, created_at)
VALUES ($1, $2, $3, $4);

-- name: GetOperator :one
SELECT * FROM operators WHERE grn = $1;

-- name: ListOperatorsByOrg :many
SELECT * FROM operators
WHERE org_grn = $1
ORDER BY grn;

-- name: AdoptOperatorCertificate :exec
INSERT INTO operator_certificates (operator_grn, serial, issued_at)
VALUES (sqlc.arg('operator_grn'), sqlc.arg('serial'), sqlc.arg('issued_at'))
ON CONFLICT (operator_grn) DO UPDATE
    SET serial = EXCLUDED.serial, issued_at = EXCLUDED.issued_at;

-- Moves the record only where it still holds the serial the caller
-- presented, so two holders of one identity cannot both carry the
-- lineage on. An operator with no row yet is adopted by the insert.
-- name: AdvanceOperatorCertificate :execrows
INSERT INTO operator_certificates (operator_grn, serial, issued_at)
VALUES (sqlc.arg('operator_grn'), sqlc.arg('serial'), sqlc.arg('issued_at'))
ON CONFLICT (operator_grn) DO UPDATE
    SET serial = EXCLUDED.serial, issued_at = EXCLUDED.issued_at
    WHERE operator_certificates.serial = sqlc.arg('predecessor');
