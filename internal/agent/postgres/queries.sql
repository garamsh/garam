-- name: LockExecution :one
SELECT * FROM agent_executions WHERE agent_grn = $1 FOR UPDATE;

-- name: GetExecution :one
SELECT e.agent_grn, e.mode, e.lineage, e.activation_id, e.latest_activation_id,
       a.generation AS current_generation
FROM agent_executions e
LEFT JOIN agent_activations a ON a.id = e.activation_id
WHERE e.agent_grn = $1;

-- ShareAssignment is taken after LockExecution, never before: the lock
-- order is agent_executions, then agent_assignments.
-- name: ShareAssignment :one
SELECT operator_grn, epoch FROM agent_assignments WHERE agent_grn = $1 FOR SHARE;

-- Now is read after the lock it follows was granted, so an expiry is
-- judged at that instant and not when the transaction began.
-- name: Now :one
SELECT clock_timestamp()::timestamptz;

-- Introspect answers in one statement, so from one snapshot, the
-- assignment, the execution and whether the certificate is current.
-- name: Introspect :one
SELECT e.mode, e.activation_id, a.generation AS current_generation,
       s.operator_grn AS assignee, s.epoch AS epoch,
       agent_credential_current(
           e.agent_grn, sqlc.arg('serial'),
           sqlc.arg('not_before')::timestamptz, sqlc.arg('not_after')::timestamptz
       )::boolean AS credential_current
FROM agent_executions e
LEFT JOIN agent_assignments s ON s.agent_grn = e.agent_grn
LEFT JOIN agent_activations a ON a.id = e.activation_id
WHERE e.agent_grn = sqlc.arg('agent_grn');

-- name: CredentialCurrent :one
SELECT agent_credential_current(
    sqlc.arg('agent_grn'), sqlc.arg('serial'),
    sqlc.arg('not_before')::timestamptz, sqlc.arg('not_after')::timestamptz
)::boolean;

-- name: Admit :one
SELECT agent_execution_admit(
    sqlc.arg('agent_grn'), sqlc.narg('token_hash')::bytea, sqlc.arg('serial'),
    sqlc.arg('not_before')::timestamptz, sqlc.arg('not_after')::timestamptz
)::text;

-- name: GetActivationByRequest :one
SELECT * FROM agent_activations WHERE agent_grn = $1 AND request_id = $2;

-- name: GetActivation :one
SELECT * FROM agent_activations WHERE agent_grn = $1 AND id = $2;

-- name: InsertActivation :one
INSERT INTO agent_activations (
    id, agent_grn, request_id, generation, epoch, lineage, audience_grn,
    grant_id, operation_ref, replaces_activation_id, token_version, created_at
) VALUES (
    sqlc.arg('id'), sqlc.arg('agent_grn'), sqlc.arg('request_id'),
    sqlc.arg('generation'), sqlc.arg('epoch'), sqlc.narg('lineage'),
    sqlc.arg('audience_grn'), sqlc.arg('grant_id'), sqlc.narg('operation_ref'),
    sqlc.narg('replaces_activation_id'), 1, sqlc.arg('created_at')
)
RETURNING *;

-- name: EndActivation :exec
UPDATE agent_activations SET ended_reason = $2, ended_at = $3
WHERE id = $1 AND ended_reason IS NULL;

-- name: RemintActivation :one
UPDATE agent_activations SET token_version = token_version + 1
WHERE id = $1
RETURNING *;

-- name: InstallActivation :exec
UPDATE agent_executions SET
    mode                 = 'fenced',
    activation_id        = sqlc.arg('activation_id'),
    latest_activation_id = sqlc.arg('activation_id'),
    token_hash           = sqlc.arg('token_hash'),
    updated_at           = sqlc.arg('updated_at')
WHERE agent_grn = sqlc.arg('agent_grn');

-- name: ReplaceToken :exec
UPDATE agent_executions SET token_hash = $2, updated_at = $3
WHERE agent_grn = $1;

-- name: ClearActivation :exec
UPDATE agent_executions SET activation_id = NULL, token_hash = NULL, updated_at = $2
WHERE agent_grn = $1;

-- name: MoveLineage :exec
UPDATE agent_executions SET
    lineage       = sqlc.arg('lineage'),
    activation_id = NULL,
    token_hash    = NULL,
    updated_at    = sqlc.arg('updated_at')
WHERE agent_grn = sqlc.arg('agent_grn');

-- name: InsertCredential :exec
INSERT INTO agent_credentials (agent_grn, serial, lineage, not_after)
VALUES ($1, $2, $3, $4);

-- Expired credentials admit nothing, so dropping them changes no
-- answer; it keeps a lineage's rows to the certificates still valid.
-- name: PruneCredentials :exec
DELETE FROM agent_credentials
WHERE agent_grn = $1 AND not_after <= clock_timestamp();

-- name: GetRecovery :one
SELECT * FROM agent_recoveries WHERE agent_grn = $1 AND request_id = $2;

-- name: InsertRecovery :exec
INSERT INTO agent_recoveries (
    agent_grn, request_id, operation_ref, body_sha256, lineage,
    certificate_pem, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- A managed creation writes these four in one transaction, the
-- execution row before the assignment: the lock order every
-- transaction follows (ADR-0080).
-- name: CreateManagedAgent :exec
INSERT INTO agents (grn, org_grn, provisioning_state, runtime_state, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $5);

-- The execution is fenced from birth in a lineage holding no
-- credential, so agent_credential_current refuses every certificate
-- of the agent until its initial issuance records one.
-- name: CreateFencedExecution :exec
INSERT INTO agent_executions (agent_grn, mode, lineage, updated_at)
VALUES ($1, 'fenced', $2, $3);

-- name: AssignFirst :one
INSERT INTO agent_assignments (agent_grn, operator_grn, epoch, created_at, updated_at)
VALUES ($1, $2, 1, $3, $3)
RETURNING epoch;

-- name: InsertManagedCreation :exec
INSERT INTO agent_managed_creations (
    agent_grn, audience_grn, controller_grn, request_id, operation_ref,
    epoch, lineage, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: GetManagedCreation :one
SELECT * FROM agent_managed_creations WHERE agent_grn = $1;

-- name: GetManagedCreationByReference :one
SELECT * FROM agent_managed_creations WHERE operation_ref = $1;

-- name: GetInitialCertificate :one
SELECT * FROM agent_initial_certificates WHERE agent_grn = $1;

-- name: InsertInitialCertificate :exec
INSERT INTO agent_initial_certificates (
    agent_grn, request_id, operation_ref, epoch, request_sha256, lineage,
    certificate_pem, issuer_pem, server_root_pem, not_after, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);
