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

-- name: GetAgentExecutionMode :one
SELECT mode FROM agent_executions WHERE agent_grn = $1;

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

-- A definition's operator and values are written once and never
-- rewritten: garam cannot tell whether an operator has acted on them,
-- so there is no update of either here. Its name is the organization's
-- word for the agent, which RenameAgentDefinition changes alone.
-- name: CreateAgentDefinition :exec
INSERT INTO agent_definitions (agent_grn, org_grn, operator_grn, name, agent_values, defined_by, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetAgentDefinition :one
SELECT * FROM agent_definitions WHERE agent_grn = $1;

-- name: RenameAgentDefinition :one
UPDATE agent_definitions SET name = sqlc.arg('name')
WHERE agent_grn = sqlc.arg('agent_grn')
RETURNING *;

-- name: ListAgentDefinitionsByOrg :many
SELECT * FROM agent_definitions WHERE org_grn = $1 ORDER BY agent_grn;

-- The claim state is the assignment the claim wrote: an unclaimed
-- definition has none, and a claimed one binds the operator it names.
-- An agent whose execution is fenced has cut over to the Operator
-- control service, and its definition is no longer the operator's
-- source (ADR-0077), so it is not listed.
-- name: ListAgentDefinitionsByOperator :many
SELECT d.*, a.epoch AS claim_epoch, a.created_at AS claim_created_at, a.updated_at AS claim_updated_at
FROM agent_definitions d
LEFT JOIN agent_assignments a ON a.agent_grn = d.agent_grn
LEFT JOIN agent_executions e ON e.agent_grn = d.agent_grn
WHERE d.operator_grn = $1 AND (e.mode IS NULL OR e.mode = 'legacy')
ORDER BY d.agent_grn;

-- LockAgentDefinition is the lock a claim serializes on: two claims of
-- one definition take the row lock in turn, and the second reads the
-- assignment the first wrote.
-- name: LockAgentDefinition :one
SELECT * FROM agent_definitions WHERE agent_grn = $1 FOR UPDATE;

-- name: CountAgentsByOrg :one
-- The agents and the definitions the organization holds, which
-- deleting the organization removes with it. One row, so the two
-- numbers describe one moment. An agent's assignment goes with the
-- agent and is not counted apart from it.
SELECT
    (SELECT count(*) FROM agents            a WHERE a.org_grn = $1) AS agent_count,
    (SELECT count(*) FROM agent_definitions d WHERE d.org_grn = $1) AS definition_count,
    (SELECT count(*) FROM agent_templates   t WHERE t.org_grn = $1) AS template_count;

-- name: CreateAgentTemplate :exec
INSERT INTO agent_templates (grn, org_grn, name, description, agent_values, created_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: GetAgentTemplate :one
SELECT * FROM agent_templates WHERE grn = $1;

-- name: ListAgentTemplatesByOrg :many
SELECT * FROM agent_templates WHERE org_grn = $1 ORDER BY name, grn;

-- An edit replaces what the template says. No definition references
-- the row, so the statement reaches no agent defined from it.
-- name: UpdateAgentTemplate :one
UPDATE agent_templates SET
    name         = sqlc.arg('name'),
    description  = sqlc.arg('description'),
    agent_values = sqlc.arg('agent_values'),
    updated_at   = sqlc.arg('updated_at')
WHERE grn = sqlc.arg('grn')
RETURNING *;

-- name: DeleteAgentTemplate :execrows
DELETE FROM agent_templates WHERE grn = $1;

-- AdmitExecution runs the agent execution fence (ADR-0080) as the
-- first statement of a protected write's transaction; the row it locks
-- is held until that transaction ends.
-- name: AdmitExecution :one
SELECT agent_execution_admit(
    sqlc.arg('agent_grn'), sqlc.narg('token_hash')::bytea, sqlc.arg('serial'),
    sqlc.arg('not_before')::timestamptz, sqlc.arg('not_after')::timestamptz
)::text;
