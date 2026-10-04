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
SELECT assignee_grn, epoch FROM agent_assignments WHERE agent_grn = $1 FOR SHARE;

-- Now is read after the lock it follows was granted, so an expiry is
-- judged at that instant and not when the transaction began.
-- name: Now :one
SELECT clock_timestamp()::timestamptz;

-- Introspect answers in one statement, so from one snapshot, the
-- assignment, the execution and whether the certificate is current.
-- name: Introspect :one
SELECT e.mode, e.activation_id, a.generation AS current_generation,
       s.assignee_grn AS assignee, s.epoch AS epoch,
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
INSERT INTO agent_assignments (agent_grn, assignee_grn, epoch, created_at, updated_at)
VALUES ($1, $2, 1, $3, $3)
RETURNING epoch;

-- name: InsertManagedCreation :exec
INSERT INTO agent_managed_creations (
    agent_grn, audience_grn, controller_grn, request_id, operation_ref,
    epoch, lineage, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: IsManagedCreation :one
SELECT EXISTS (SELECT 1 FROM agent_managed_creations WHERE agent_grn = $1);

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

-- Standalone approvals and supervisors (ADR-0082).

-- name: InsertEnrollment :execrows
INSERT INTO agent_enrollments (
    id, org_grn, agent_grn, supervisor_grn, host_name, supervisor_key_sha256,
    agent_key_sha256, expected_assignee_grn, expected_epoch, request_id,
    approved_by, secret_sha256, expires_at, created_at
) VALUES (
    sqlc.arg('id'), sqlc.arg('org_grn'), sqlc.arg('agent_grn'), sqlc.arg('supervisor_grn'),
    sqlc.arg('host_name'), sqlc.arg('supervisor_key_sha256'), sqlc.arg('agent_key_sha256'),
    sqlc.narg('expected_assignee_grn'), sqlc.narg('expected_epoch'), sqlc.arg('request_id'),
    sqlc.arg('approved_by'), sqlc.arg('secret_sha256'), sqlc.arg('expires_at'), sqlc.arg('created_at')
)
ON CONFLICT (org_grn, request_id) DO NOTHING;

-- name: GetEnrollmentByRequest :one
SELECT * FROM agent_enrollments WHERE org_grn = $1 AND request_id = $2;

-- name: GetEnrollment :one
SELECT * FROM agent_enrollments WHERE org_grn = $1 AND id = $2;

-- name: GetEnrollmentBySecret :one
SELECT * FROM agent_enrollments WHERE secret_sha256 = $1;

-- name: GetEnrollmentByID :one
SELECT * FROM agent_enrollments WHERE id = $1;

-- LockEnrollment is what withdrawing and spending an approval
-- serialize on, taken after the execution row where both are taken.
-- name: LockEnrollment :one
SELECT * FROM agent_enrollments WHERE id = $1 FOR UPDATE;

-- name: WithdrawEnrollment :exec
UPDATE agent_enrollments SET withdrawn_at = $2
WHERE id = $1 AND withdrawn_at IS NULL AND consumed_at IS NULL;

-- name: WithdrawPendingEnrollments :exec
UPDATE agent_enrollments SET withdrawn_at = $2
WHERE agent_grn = $1 AND withdrawn_at IS NULL AND consumed_at IS NULL;

-- name: SpendEnrollment :exec
UPDATE agent_enrollments SET
    consumed_at                = sqlc.arg('consumed_at'),
    request_sha256             = sqlc.arg('request_sha256'),
    epoch                      = sqlc.arg('epoch'),
    lineage                    = sqlc.arg('lineage'),
    supervisor_certificate_pem = sqlc.arg('supervisor_certificate_pem'),
    agent_certificate_pem      = sqlc.arg('agent_certificate_pem'),
    issuer_pem                 = sqlc.arg('issuer_pem'),
    server_root_pem            = sqlc.arg('server_root_pem')
WHERE id = sqlc.arg('id');

-- ReassignSupervisor moves a supervisor-held agent to its replacement
-- only from the assignment the approval expected; any other matches
-- no row.
-- name: ReassignSupervisor :one
UPDATE agent_assignments SET
    assignee_grn = sqlc.arg('assignee_grn'),
    epoch        = epoch + 1,
    updated_at   = sqlc.arg('updated_at')
WHERE agent_grn = sqlc.arg('agent_grn')
  AND assignee_grn = sqlc.arg('expected_assignee_grn')
  AND epoch = sqlc.arg('expected_epoch')
RETURNING epoch;

-- name: InsertSupervisor :exec
INSERT INTO agent_supervisors (grn, agent_grn, enrollment_id, host_name, serial, not_after, registered_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetSupervisor :one
SELECT * FROM agent_supervisors WHERE grn = $1;

-- name: GetSupervisorByEnrollment :one
SELECT * FROM agent_supervisors WHERE enrollment_id = $1;

-- name: LockSupervisor :one
SELECT * FROM agent_supervisors WHERE grn = $1 FOR UPDATE;

-- name: EndCurrentSupervisor :exec
UPDATE agent_supervisors SET ended_at = $2 WHERE agent_grn = $1 AND ended_at IS NULL;

-- name: EndSupervisor :exec
UPDATE agent_supervisors SET ended_at = $2 WHERE grn = $1 AND ended_at IS NULL;

-- name: MoveSupervisorCertificate :exec
UPDATE agent_supervisors SET serial = $2, not_after = $3 WHERE grn = $1;

-- name: GetSupervisorRenewal :one
SELECT r.*, (s.ended_at IS NULL AND s.serial = r.serial)::boolean AS current
FROM agent_supervisor_renewals r
JOIN agent_supervisors s ON s.grn = r.supervisor_grn
WHERE r.supervisor_grn = $1 AND r.predecessor_serial = $2;

-- name: InsertSupervisorRenewal :exec
INSERT INTO agent_supervisor_renewals (
    supervisor_grn, predecessor_serial, request_id, request_sha256, serial,
    certificate_pem, issuer_pem, server_root_pem, not_after, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);
