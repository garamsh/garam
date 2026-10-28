-- name: CreateAgent :exec
INSERT INTO agents (grn, org_grn, provisioning_state, runtime_state, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- Every agent has an execution row from the transaction that creates
-- it, so the row the fence locks always exists (ADR-0080).
-- name: CreateAgentExecution :exec
INSERT INTO agent_executions (agent_grn, updated_at) VALUES ($1, $2);

-- LockAgentExecution comes before any read or write of the assignment
-- in the same transaction: agent_executions, then agent_assignments.
-- name: LockAgentExecution :one
SELECT agent_grn FROM agent_executions WHERE agent_grn = $1 FOR UPDATE;

-- EndReassignedActivation ends the activation a reassignment leaves
-- behind: the new assignee's runtime activates its own.
-- name: EndReassignedActivation :exec
WITH ended AS (
    UPDATE agent_activations a SET ended_reason = 'reassigned', ended_at = $2
    FROM agent_executions e
    WHERE e.agent_grn = $1 AND a.id = e.activation_id AND a.ended_reason IS NULL
)
UPDATE agent_executions x SET activation_id = NULL, token_hash = NULL, updated_at = $2
WHERE x.agent_grn = $1 AND x.activation_id IS NOT NULL;

-- name: GetAgent :one
SELECT * FROM agents WHERE grn = $1;

-- Renaming changes what the organization calls the agent and nothing
-- else (ADR-0119): no assignment, lineage or token moves, and
-- updated_at, which a state report moves, stays.
-- name: RenameAgent :one
UPDATE agents SET name = sqlc.narg('name')
WHERE grn = sqlc.arg('grn')
RETURNING *;

-- name: ListAgentsByOrg :many
SELECT * FROM agents
WHERE org_grn = sqlc.arg('org_grn')
ORDER BY grn
LIMIT sqlc.narg('row_limit') OFFSET sqlc.arg('row_offset');

-- AssignAgent writes onto the assignment the agent already has, so a
-- reassignment moves the assignee and raises the epoch that fenced the
-- one it replaces.
-- name: AssignAgent :one
INSERT INTO agent_assignments (agent_grn, assignee_grn, epoch, created_at, updated_at)
VALUES (sqlc.arg('agent_grn'), sqlc.arg('assignee_grn'), 1, sqlc.arg('created_at'), sqlc.arg('updated_at'))
ON CONFLICT (agent_grn) DO UPDATE SET
    assignee_grn = excluded.assignee_grn,
    epoch        = agent_assignments.epoch + 1,
    updated_at   = excluded.updated_at
RETURNING *;

-- name: GetAgentAssignment :one
SELECT * FROM agent_assignments WHERE agent_grn = $1;

-- name: ListAgentAssignmentsByOrg :many
SELECT a.* FROM agent_assignments a
JOIN agents g ON g.grn = a.agent_grn
WHERE g.org_grn = $1
ORDER BY a.agent_grn;

-- ReportAgentProvisioningState and ReportAgentRuntimeState write at the
-- epoch the agent is currently assigned at; any other epoch matches no
-- row, and an agent with no assignment matches none either.
-- name: ReportAgentProvisioningState :one
UPDATE agents SET
    provisioning_state = sqlc.arg('provisioning_state'),
    updated_at         = sqlc.arg('updated_at')
WHERE grn = sqlc.arg('grn')
  AND EXISTS (
      SELECT 1 FROM agent_assignments a
      WHERE a.agent_grn = agents.grn AND a.epoch = sqlc.arg('epoch')
  )
RETURNING *;

-- name: ReportAgentRuntimeState :one
UPDATE agents SET
    runtime_state = sqlc.arg('runtime_state'),
    updated_at    = sqlc.arg('updated_at')
WHERE grn = sqlc.arg('grn')
  AND EXISTS (
      SELECT 1 FROM agent_assignments a
      WHERE a.agent_grn = agents.grn AND a.epoch = sqlc.arg('epoch')
  )
RETURNING *;

-- SearchAgentsByOrg answers the org's agents whose identifier (the
-- GRN's fifth segment) matches pattern, case aside, ordered by GRN.
-- name: SearchAgentsByOrg :many
SELECT * FROM agents
WHERE org_grn = sqlc.arg('org_grn')
  AND (split_part(grn, ':', 5) ILIKE sqlc.arg('pattern') OR name ILIKE sqlc.arg('pattern'))
ORDER BY grn;

-- name: CountAgentsByOrg :one
-- The agents the organization holds, which deleting the organization
-- removes with it. An agent's assignment goes with the agent and is
-- not counted apart from it.
SELECT count(*) AS agent_count FROM agents WHERE org_grn = $1;

-- AdmitExecution runs the agent execution fence (ADR-0080) as the
-- first statement of a protected write's transaction; the row it locks
-- is held until that transaction ends.
-- name: AdmitExecution :one
SELECT agent_execution_admit(
    sqlc.arg('agent_grn'), sqlc.narg('token_hash')::bytea, sqlc.arg('serial'),
    sqlc.arg('not_before')::timestamptz, sqlc.arg('not_after')::timestamptz
)::text;

-- The execution a token is minted against, held FOR SHARE so a
-- recovery committing first is seen and one committing after waits
-- for the mint (ADR-0100). The assignment is read by a statement of
-- its own after it, so its snapshot is taken once this lock is
-- granted.
-- name: ShareAgentLineage :one
SELECT lineage FROM agent_executions WHERE agent_grn = $1 FOR SHARE;

-- name: RecordAgentToken :exec
INSERT INTO agent_tokens (token_hash, serial, agent_grn, assignee_grn, epoch, lineage,
                          audience_grn, issued_at, expires_at)
VALUES (sqlc.arg('token_hash'), sqlc.arg('serial'), sqlc.arg('agent_grn'), sqlc.arg('assignee_grn'),
        sqlc.arg('epoch'), sqlc.narg('lineage'), sqlc.narg('audience_grn'),
        sqlc.arg('issued_at'), sqlc.arg('expires_at'));

-- Keeps an agent's newest live tokens for the audience just minted
-- for, and its newest live ones in all, which bounds what an
-- assignee's mints can write (ADR-0100), and deletes the rest, the
-- expired ones included, so it is the mint's sweep too. It takes them
-- in token_hash order, as an operator's deletion takes the tokens
-- naming it (000047), and is the one statement of a mint that waits
-- on a token (#1434).
-- name: TrimAgentTokens :exec
WITH audience_kept AS (
    SELECT k.token_hash FROM agent_tokens k
    WHERE k.agent_grn = sqlc.arg('agent_grn')
      AND k.audience_grn IS NOT DISTINCT FROM sqlc.narg('audience_grn')
      AND k.expires_at > now()
    ORDER BY k.issued_at DESC, k.token_hash DESC
    LIMIT sqlc.arg('keep_per_audience')
), kept AS (
    SELECT k.token_hash FROM agent_tokens k
    WHERE k.agent_grn = sqlc.arg('agent_grn') AND k.expires_at > now()
      AND (k.audience_grn IS DISTINCT FROM sqlc.narg('audience_grn')
           OR k.token_hash IN (SELECT a.token_hash FROM audience_kept a))
    ORDER BY k.issued_at DESC, k.token_hash DESC
    LIMIT sqlc.arg('keep_in_all')
), doomed AS MATERIALIZED (
    SELECT t.token_hash FROM agent_tokens t
    WHERE t.agent_grn = sqlc.arg('agent_grn')
      AND t.token_hash NOT IN (SELECT c.token_hash FROM kept c)
    ORDER BY t.token_hash
    FOR UPDATE OF t
)
DELETE FROM agent_tokens WHERE token_hash IN (SELECT d.token_hash FROM doomed d);

-- The read every request presenting an agent token takes (ADR-0100):
-- the token's row joined to the agent's assignment and execution as
-- they stand now, so a reassignment or a recovery refuses it from the
-- next request.
-- name: ResolveAgentToken :one
SELECT t.agent_grn, t.serial, t.audience_grn, t.issued_at, t.expires_at
FROM agent_tokens t
JOIN agent_assignments a
  ON a.agent_grn = t.agent_grn AND a.assignee_grn = t.assignee_grn AND a.epoch = t.epoch
JOIN agent_executions e
  ON e.agent_grn = t.agent_grn AND t.lineage IS NOT DISTINCT FROM e.lineage
WHERE t.token_hash = $1 AND t.expires_at > now();

-- A token as it was minted to be shown to one audience, with no join to
-- the agent's current state: the execution fence judges that, so a
-- token of an earlier epoch or lineage is answered fenced while its row
-- is kept. A mint's sweep and trim delete rows, and a token whose row
-- is gone is not proven.
--
-- A token the caller itself minted for garam proves the agent too: a
-- standalone host's supervisor is the assignee and the lifecycle
-- authority at once, and forwards the agent's own token (ADR-0100).
-- name: GetAgentTokenShownTo :one
SELECT agent_grn, serial, audience_grn, issued_at, expires_at
FROM agent_tokens
WHERE token_hash = sqlc.arg('token_hash')
  AND (audience_grn = sqlc.arg('caller_grn')
       OR (audience_grn IS NULL AND assignee_grn = sqlc.arg('caller_grn')));

-- GetLatestGeneration answers the generation of the agent's most
-- recent activation, current or ended; no row for an agent never
-- activated.
-- name: GetLatestGeneration :one
SELECT a.generation FROM agent_executions e
JOIN agent_activations a ON a.id = e.latest_activation_id
WHERE e.agent_grn = $1;

-- ListLatestGenerationsByOrg answers GetLatestGeneration for each of
-- the organization's agents that was ever activated.
-- name: ListLatestGenerationsByOrg :many
SELECT e.agent_grn, a.generation FROM agent_executions e
JOIN agent_activations a ON a.id = e.latest_activation_id
JOIN agents g ON g.grn = e.agent_grn
WHERE g.org_grn = $1
ORDER BY e.agent_grn;

-- name: LockExecution :one
SELECT * FROM agent_executions WHERE agent_grn = $1 FOR UPDATE;

-- name: GetExecution :one
SELECT e.agent_grn, e.lineage, e.activation_id, e.latest_activation_id,
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
SELECT e.activation_id, a.generation AS current_generation,
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

-- name: GetRecovery :one
SELECT * FROM agent_recoveries WHERE agent_grn = $1 AND request_id = $2;

-- name: InsertRecovery :exec
INSERT INTO agent_recoveries (
    agent_grn, request_id, operation_ref, body_sha256, lineage, created_at
) VALUES ($1, $2, $3, $4, $5, $6);

-- A managed creation writes these four in one transaction, the
-- execution row before the assignment: the lock order every
-- transaction follows (ADR-0080).
-- name: CreateManagedAgent :exec
INSERT INTO agents (grn, org_grn, provisioning_state, runtime_state, created_at, updated_at, name)
VALUES ($1, $2, $3, $4, $5, $5, sqlc.narg('name'));

-- The execution starts in a lineage holding no credential, so
-- agent_credential_current refuses every certificate of the agent
-- until its initial issuance records one.
-- name: CreateFencedExecution :exec
INSERT INTO agent_executions (agent_grn, lineage, updated_at)
VALUES ($1, $2, $3);

-- name: AssignFirst :one
INSERT INTO agent_assignments (agent_grn, assignee_grn, epoch, created_at, updated_at)
VALUES ($1, $2, 1, $3, $3)
RETURNING epoch;

-- name: InsertManagedCreation :exec
INSERT INTO agent_managed_creations (
    agent_grn, audience_grn, controller_grn, request_id, operation_ref,
    epoch, lineage, created_at, name
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, sqlc.narg('name'));

-- name: IsManagedCreation :one
SELECT EXISTS (SELECT 1 FROM agent_managed_creations WHERE agent_grn = $1);

-- name: GetManagedCreation :one
SELECT * FROM agent_managed_creations WHERE agent_grn = $1;

-- name: GetManagedCreationByReference :one
SELECT * FROM agent_managed_creations WHERE operation_ref = $1;

-- Standalone approvals and supervisors (ADR-0082).

-- name: InsertEnrollment :execrows
INSERT INTO agent_enrollments (
    id, org_grn, agent_grn, supervisor_grn, host_name,
    expected_assignee_grn, expected_epoch, request_id,
    approved_by, secret_sha256, expires_at, created_at, placement
) VALUES (
    sqlc.arg('id'), sqlc.arg('org_grn'), sqlc.arg('agent_grn'), sqlc.arg('supervisor_grn'),
    sqlc.arg('host_name'), sqlc.narg('expected_assignee_grn'), sqlc.narg('expected_epoch'), sqlc.arg('request_id'),
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
INSERT INTO agent_supervisors (grn, agent_grn, enrollment_id, host_name, registered_at)
VALUES ($1, $2, $3, $4, $5);

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

-- An agent-enrollment.v1 approval (ADR-0102), with the agent and
-- assignment a replacement expects (ADR-0107). Either unique key
-- refusing the row writes nothing, and the caller reads what holds it.
-- name: InsertEnrollmentApproval :execrows
INSERT INTO agent_enrollment_approvals (
    id, org_grn, request_id, placement, audience_grn, assignee_grn, operation_ref, created_at,
    replaced_agent_grn, expected_assignee_grn, expected_epoch, name
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, sqlc.narg('name'))
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
