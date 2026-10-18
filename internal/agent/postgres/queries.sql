-- name: LockExecution :one
SELECT * FROM agent_executions WHERE agent_grn = $1 FOR UPDATE;

-- name: GetExecution :one
SELECT e.agent_grn, e.mode, e.lineage, e.activation_id, e.latest_activation_id,
       a.generation AS current_generation
FROM agent_executions e
LEFT JOIN agent_activations a ON a.id = e.activation_id
WHERE e.agent_grn = $1;

-- ShareAssignment is taken after LockExecution, never before: the lock
-- order is agent_executions, then agent_assignments. A supervisor
-- assignee is registered only while its registration is current, which
-- a revocation or a replacement ends under the same execution lock
-- (ADR-0082); an operator's record is read on the listener instead.
-- name: ShareAssignment :one
SELECT a.assignee_grn, a.epoch,
       (split_part(a.assignee_grn, ':', 4) <> 'supervisor' OR EXISTS (
           SELECT 1 FROM agent_supervisors s
           WHERE s.grn = a.assignee_grn AND s.agent_grn = a.agent_grn AND s.ended_at IS NULL
       ))::boolean AS assignee_registered
FROM agent_assignments a WHERE a.agent_grn = $1 FOR SHARE OF a;

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
    certificate_pem, issuer_pem, server_root_pem, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

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
    approved_by, secret_sha256, expires_at, created_at, placement
) VALUES (
    sqlc.arg('id'), sqlc.arg('org_grn'), sqlc.arg('agent_grn'), sqlc.arg('supervisor_grn'),
    sqlc.arg('host_name'), sqlc.arg('supervisor_key_sha256'), sqlc.arg('agent_key_sha256'),
    sqlc.narg('expected_assignee_grn'), sqlc.narg('expected_epoch'), sqlc.arg('request_id'),
    sqlc.arg('approved_by'), sqlc.arg('secret_sha256'), sqlc.arg('expires_at'), sqlc.arg('created_at'),
    sqlc.arg('placement')
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
    server_root_pem            = sqlc.arg('server_root_pem'),
    replaced_activation_id     = sqlc.narg('replaced_activation_id')
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

-- A supervisor's credential and tokens under standalone-enrollment.v2
-- (ADR-0100 G1d). Every read joins the registration and requires it
-- not to have ended, so a revocation or a replacement refuses the
-- credential and every token from the next request.

-- A retried spending re-issues the credential: deleting the row takes
-- every token minted under it.
-- name: DeleteSupervisorCredential :exec
DELETE FROM supervisor_credentials WHERE supervisor_grn = $1;

-- name: InsertSupervisorCredential :exec
INSERT INTO supervisor_credentials (supervisor_grn, credential_hash, issued_at) VALUES ($1, $2, $3);

-- name: GetSupervisorCredentialHolder :one
SELECT c.id, c.supervisor_grn
FROM supervisor_credentials c
JOIN agent_supervisors s ON s.grn = c.supervisor_grn
WHERE c.credential_hash = $1 AND s.ended_at IS NULL;

-- An exchange holds the credential's row while it records and trims,
-- so two exchanges at once leave no more live tokens than the bound.
-- name: LockSupervisorCredential :one
SELECT id FROM supervisor_credentials WHERE id = $1 FOR UPDATE;

-- name: RecordSupervisorToken :exec
WITH swept AS (
    DELETE FROM supervisor_tokens
    WHERE credential_id = sqlc.arg('credential_id') AND expires_at <= now()
)
INSERT INTO supervisor_tokens (token_hash, credential_id, issued_at, expires_at)
VALUES (sqlc.arg('token_hash'), sqlc.arg('credential_id'), sqlc.arg('issued_at'), sqlc.arg('expires_at'));

-- name: TrimSupervisorTokens :exec
DELETE FROM supervisor_tokens t
WHERE t.credential_id = sqlc.arg('credential_id')
  AND t.token_hash NOT IN (
      SELECT k.token_hash FROM supervisor_tokens k
      WHERE k.credential_id = sqlc.arg('credential_id')
      ORDER BY k.issued_at DESC, k.token_hash DESC
      LIMIT sqlc.arg('keep'));

-- name: ResolveSupervisorToken :one
SELECT c.supervisor_grn, t.issued_at, t.expires_at
FROM supervisor_tokens t
JOIN supervisor_credentials c ON c.id = t.credential_id
JOIN agent_supervisors s ON s.grn = c.supervisor_grn
WHERE t.token_hash = $1 AND t.expires_at > now() AND s.ended_at IS NULL;

-- Cutover (ADR-0086). Every stage takes the execution row FOR UPDATE
-- first, then the definition row: the lock a claim serializes on.

-- name: LockDefinition :one
SELECT * FROM agent_definitions WHERE agent_grn = $1 FOR UPDATE;

-- name: GetCutoverSource :one
SELECT * FROM agent_definitions WHERE agent_grn = $1;

-- name: GetCutoverAttempt :one
SELECT * FROM agent_cutovers WHERE agent_grn = $1 AND import_id = $2;

-- name: GetOpenCutover :one
SELECT * FROM agent_cutovers WHERE agent_grn = $1 AND stage <> 'rolled_back';

-- name: GetLatestCutover :one
SELECT * FROM agent_cutovers WHERE agent_grn = $1
ORDER BY created_at DESC, import_id DESC LIMIT 1;

-- name: InsertCutover :one
INSERT INTO agent_cutovers (
    agent_grn, import_id, stage, frozen_digest, audience_grn, grant_id,
    operation_ref, created_at
) VALUES ($1, $2, 'frozen', $3, $4, $5, $6, $7)
RETURNING *;

-- name: SwitchCutover :one
UPDATE agent_cutovers SET stage = 'switched', switched_at = $3
WHERE agent_grn = $1 AND import_id = $2 AND stage = 'frozen'
RETURNING *;

-- name: RollBackCutover :one
UPDATE agent_cutovers SET stage = 'rolled_back', ended_at = $3
WHERE agent_grn = $1 AND import_id = $2 AND stage = 'frozen'
RETURNING *;

-- IsCutOver reads whether the agent's cutover is switched. It is a
-- statement of its own after the execution lock, so its snapshot is
-- taken once that lock is granted.
-- name: IsCutOver :one
SELECT EXISTS (
    SELECT 1 FROM agent_cutovers WHERE agent_grn = $1 AND stage = 'switched'
);

-- name: GetCutoverAgent :one
SELECT e.mode, s.assignee_grn, s.epoch
FROM agent_executions e
JOIN agent_assignments s ON s.agent_grn = e.agent_grn
WHERE e.agent_grn = $1;

-- The inventory reads an organization from one snapshot: every agent
-- with its execution, assignment and definition, each agent's most
-- recent attempt, every definition no operator claimed, and every
-- template.
-- name: InventoryAgents :many
SELECT a.grn, e.mode, e.lineage, s.assignee_grn, s.epoch,
       d.name AS definition_name, d.operator_grn AS definition_operator_grn,
       d.agent_values AS definition_values,
       (SELECT count(*) FROM agent_credentials c WHERE c.agent_grn = a.grn) AS tracked_credentials
FROM agents a
JOIN agent_executions e ON e.agent_grn = a.grn
LEFT JOIN agent_assignments s ON s.agent_grn = a.grn
LEFT JOIN agent_definitions d ON d.agent_grn = a.grn
WHERE a.org_grn = $1
ORDER BY a.grn;

-- name: InventoryAttempts :many
SELECT DISTINCT ON (c.agent_grn) c.agent_grn, c.import_id, c.stage
FROM agent_cutovers c
JOIN agents a ON a.grn = c.agent_grn
WHERE a.org_grn = $1
ORDER BY c.agent_grn, c.created_at DESC, c.import_id DESC;

-- name: InventoryUnclaimedDefinitions :many
SELECT d.* FROM agent_definitions d
WHERE d.org_grn = $1 AND NOT EXISTS (SELECT 1 FROM agents a WHERE a.grn = d.agent_grn)
ORDER BY d.agent_grn;

-- name: InventoryTemplates :many
SELECT t.grn, t.name, t.agent_values FROM agent_templates t
WHERE t.org_grn = $1
ORDER BY t.grn;

-- An agent-enrollment.v1 approval (ADR-0102), with the agent and
-- assignment a replacement expects (ADR-0107). Either unique key
-- refusing the row writes nothing, and the caller reads what holds it.
-- name: InsertEnrollmentApproval :execrows
INSERT INTO agent_enrollment_approvals (
    id, org_grn, request_id, placement, audience_grn, assignee_grn, operation_ref, created_at,
    replaced_agent_grn, expected_assignee_grn, expected_epoch
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT DO NOTHING;

-- name: GetEnrollmentApprovalByRequest :one
SELECT * FROM agent_enrollment_approvals WHERE org_grn = $1 AND request_id = $2;

-- name: GetEnrollmentApproval :one
SELECT * FROM agent_enrollment_approvals WHERE org_grn = $1 AND id = $2;

-- LockEnrollmentApproval is what concurrent admissions of one approval
-- serialize on: the first spends it and the rest read what it wrote.
-- name: LockEnrollmentApproval :one
SELECT * FROM agent_enrollment_approvals WHERE id = $1 FOR UPDATE;

-- name: SpendEnrollmentApproval :exec
UPDATE agent_enrollment_approvals SET agent_grn = $2, admitted_at = $3 WHERE id = $1;

-- name: SpendReplacementApproval :exec
UPDATE agent_enrollment_approvals SET admitted_epoch = $2, admitted_lineage = $3, admitted_at = $4
WHERE id = $1;

-- The admitted replacement a reference spent, which a first activation
-- after it may carry (ADR-0107).
-- name: GetAdmittedReplacementByReference :one
SELECT * FROM agent_enrollment_approvals
WHERE operation_ref = $1 AND replaced_agent_grn IS NOT NULL AND admitted_at IS NOT NULL;

-- ListEnrollments pages an organization's approvals oldest first,
-- selecting no secret hash, key fingerprint, request digest or
-- certificate: what an administrator reads of them in a list.
-- name: ListEnrollments :many
SELECT id, placement, agent_grn, supervisor_grn, host_name, expected_assignee_grn, expected_epoch,
       approved_by, expires_at, created_at, withdrawn_at, consumed_at, epoch
FROM agent_enrollments
WHERE org_grn = sqlc.arg('org_grn')
ORDER BY created_at, id
LIMIT sqlc.narg('row_limit') OFFSET sqlc.arg('row_offset');

-- ListSupervisors pages an organization's supervisor registrations,
-- current and ended, oldest first, with the epoch their enrollment
-- assigned and the latest activation of their agent at that epoch.
-- It selects no serial or credential.
-- name: ListSupervisors :many
SELECT s.grn, s.agent_grn, s.enrollment_id, s.host_name, s.registered_at, s.ended_at,
       e.placement, e.epoch,
       (SELECT max(a.created_at) FROM agent_activations a
        WHERE a.agent_grn = s.agent_grn AND a.epoch = e.epoch)::timestamptz AS last_activated_at
FROM agent_supervisors s
JOIN agent_enrollments e ON e.id = s.enrollment_id
WHERE e.org_grn = sqlc.arg('org_grn')
ORDER BY s.registered_at, s.grn
LIMIT sqlc.narg('row_limit') OFFSET sqlc.arg('row_offset');
