-- name: CreateAgent :exec
INSERT INTO agents (grn, org_grn, provisioning_state, runtime_state, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetAgent :one
SELECT * FROM agents WHERE grn = $1;

-- name: ListAgentsByOrg :many
SELECT * FROM agents WHERE org_grn = $1 ORDER BY grn;

-- AssignAgent writes onto the assignment the agent already has, so a
-- reassignment moves the operator and raises the epoch that fenced the
-- one it replaces.
-- name: AssignAgent :one
INSERT INTO agent_assignments (agent_grn, operator_grn, epoch, created_at, updated_at)
VALUES (sqlc.arg('agent_grn'), sqlc.arg('operator_grn'), 1, sqlc.arg('created_at'), sqlc.arg('updated_at'))
ON CONFLICT (agent_grn) DO UPDATE SET
    operator_grn = excluded.operator_grn,
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

-- A definition is written once and never rewritten: garam cannot tell
-- whether an operator has acted on one, so there is no update here.
-- name: CreateAgentDefinition :exec
INSERT INTO agent_definitions (agent_grn, org_grn, operator_grn, agent_values, defined_by, created_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListAgentDefinitionsByOrg :many
SELECT * FROM agent_definitions WHERE org_grn = $1 ORDER BY agent_grn;
