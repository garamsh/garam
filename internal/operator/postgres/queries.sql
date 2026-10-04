-- name: GetDelegationByRequest :one
SELECT * FROM operator_delegations
WHERE org_grn = $1 AND operator_grn = $2 AND request_id = $3;

-- A grant write locks the current grant before replacing it, so a
-- second writer holding the same view waits and then finds it ended.
-- name: LockCurrentDelegation :one
SELECT * FROM operator_delegations
WHERE org_grn = $1 AND operator_grn = $2 AND ended_at IS NULL
FOR UPDATE;

-- name: GetCurrentDelegation :one
SELECT * FROM operator_delegations
WHERE org_grn = $1 AND operator_grn = $2 AND ended_at IS NULL;

-- name: GetDelegation :one
SELECT * FROM operator_delegations WHERE id = $1;

-- name: CreateDelegation :exec
INSERT INTO operator_delegations
    (id, org_grn, operator_grn, request_id, replaces, operations, expires_at, granted_by, granted_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: AddDelegationControllers :exec
INSERT INTO operator_delegation_controllers (delegation_id, controller_grn)
SELECT sqlc.arg('delegation_id'), unnest(sqlc.arg('controller_grns')::text[]);

-- name: ListDelegationControllers :many
SELECT controller_grn FROM operator_delegation_controllers
WHERE delegation_id = $1
ORDER BY controller_grn;

-- Ends the named grant only, so a withdrawal retried after a
-- replacement never ends the replacement.
-- name: EndDelegation :execrows
UPDATE operator_delegations SET ended_at = sqlc.arg('ended_at')
WHERE id = sqlc.arg('id') AND org_grn = sqlc.arg('org_grn')
  AND operator_grn = sqlc.arg('operator_grn') AND ended_at IS NULL;

-- A request identifier holds one logical operation. A remint of the
-- same binding replaces its token and answers its reference; any other
-- binding under the identifier updates nothing and returns no row,
-- which the caller reads as a conflict. One statement, so concurrent
-- mints of one identifier leave one row.
-- name: MintOperationAuthority :one
INSERT INTO operation_authorities
    (ref, org_grn, audience_grn, request_id, delegation_id, actor_grn, operation,
     target_grn, assignee_grn, assignment_epoch, body_sha256, token_hash,
     token_expires_at, created_at)
VALUES (sqlc.arg('ref'), sqlc.arg('org_grn'), sqlc.arg('audience_grn'), sqlc.arg('request_id'),
        sqlc.arg('delegation_id'), sqlc.arg('actor_grn'), sqlc.arg('operation'),
        sqlc.arg('target_grn'), sqlc.narg('assignee_grn'), sqlc.narg('assignment_epoch'),
        sqlc.arg('body_sha256'), sqlc.arg('token_hash'), sqlc.arg('token_expires_at'),
        sqlc.arg('created_at'))
ON CONFLICT (org_grn, audience_grn, request_id) DO UPDATE
    SET token_hash = EXCLUDED.token_hash, token_expires_at = EXCLUDED.token_expires_at
    WHERE operation_authorities.delegation_id = EXCLUDED.delegation_id
      AND operation_authorities.actor_grn = EXCLUDED.actor_grn
      AND operation_authorities.operation = EXCLUDED.operation
      AND operation_authorities.target_grn = EXCLUDED.target_grn
      AND operation_authorities.body_sha256 = EXCLUDED.body_sha256
      AND operation_authorities.assignee_grn IS NOT DISTINCT FROM EXCLUDED.assignee_grn
      AND operation_authorities.assignment_epoch IS NOT DISTINCT FROM EXCLUDED.assignment_epoch
RETURNING ref;

-- name: GetOperationAuthorityByToken :one
SELECT * FROM operation_authorities WHERE token_hash = $1;

-- name: GetOperationAuthority :one
SELECT * FROM operation_authorities WHERE ref = $1;
