-- name: CreateAgent :exec
INSERT INTO agents (grn, org_grn, provisioning_state, runtime_state, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetAgent :one
SELECT * FROM agents WHERE grn = $1;

-- name: ListAgentsByOrg :many
SELECT * FROM agents
WHERE org_grn = sqlc.arg('org_grn')
ORDER BY grn
LIMIT sqlc.narg('row_limit') OFFSET sqlc.arg('row_offset');

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
-- row, and an agent with no assignment matches none either. Each moves
-- the health report time, which creation does not write (ADR-0076).
-- name: ReportAgentProvisioningState :one
UPDATE agents SET
    provisioning_state = sqlc.arg('provisioning_state'),
    updated_at         = sqlc.arg('updated_at'),
    health_reported_at = sqlc.arg('updated_at')
WHERE grn = sqlc.arg('grn')
  AND EXISTS (
      SELECT 1 FROM agent_assignments a
      WHERE a.agent_grn = agents.grn AND a.epoch = sqlc.arg('epoch')
  )
RETURNING *;

-- name: ReportAgentRuntimeState :one
UPDATE agents SET
    runtime_state      = sqlc.arg('runtime_state'),
    updated_at         = sqlc.arg('updated_at'),
    health_reported_at = sqlc.arg('updated_at')
WHERE grn = sqlc.arg('grn')
  AND EXISTS (
      SELECT 1 FROM agent_assignments a
      WHERE a.agent_grn = agents.grn AND a.epoch = sqlc.arg('epoch')
  )
RETURNING *;

-- ReportAgentConfiguration records the operator's acknowledgement of a
-- revision, where that operator holds the agent at the epoch the report
-- carries: the two are compared together in this statement, so an
-- operator a reassignment replaced cannot land a report under its
-- successor's epoch. It matches no row for a revision the definition
-- has not reached, nor for one
-- older than what the same epoch already acknowledged; an
-- acknowledgement made at an earlier epoch is no bound, because it was
-- another assignment's (ADR-0076). The same revision again replaces the
-- last report, in the order garam receives them.
-- name: ReportAgentConfiguration :one
UPDATE agents SET
    configuration_epoch        = sqlc.arg('epoch')::integer,
    configuration_revision     = sqlc.arg('revision')::integer,
    configuration_outcome      = sqlc.arg('outcome')::text,
    configuration_reason       = sqlc.narg('reason')::text,
    configuration_ignored_keys = sqlc.narg('ignored_keys')::text[],
    configuration_reported_at  = sqlc.arg('reported_at')::timestamptz
WHERE grn = sqlc.arg('grn')
  AND EXISTS (
      SELECT 1 FROM agent_assignments a
      WHERE a.agent_grn = agents.grn
        AND a.epoch = sqlc.arg('epoch')::integer
        AND a.operator_grn = sqlc.arg('operator_grn')::text
  )
  AND EXISTS (
      SELECT 1 FROM agent_definitions d
      WHERE d.agent_grn = agents.grn
        AND d.revision >= sqlc.arg('revision')::integer
  )
  AND (configuration_epoch IS DISTINCT FROM sqlc.arg('epoch')::integer
       OR configuration_revision <= sqlc.arg('revision')::integer)
RETURNING *;

-- A definition's operator is written once and never rewritten: garam
-- cannot move an agent between operators here. Its values are revised
-- by ReviseAgentDefinitionValues, each save a new revision (ADR-0076),
-- and its name is the organization's word for the agent, which
-- RenameAgentDefinition changes alone (ADR-0075).
-- name: CreateAgentDefinition :exec
INSERT INTO agent_definitions (agent_grn, org_grn, operator_grn, name, agent_values, defined_by, created_at, revision, revised_at, revised_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: GetAgentDefinition :one
SELECT * FROM agent_definitions WHERE agent_grn = $1;

-- ReviseAgentDefinitionValues replaces the values only where the
-- definition is still at the revision the editor read, so of two
-- editors who read one revision the second matches no row.
-- name: ReviseAgentDefinitionValues :one
UPDATE agent_definitions SET
    agent_values = sqlc.arg('agent_values'),
    revision     = revision + 1,
    revised_at   = sqlc.arg('revised_at'),
    revised_by   = sqlc.arg('revised_by')
WHERE agent_grn = sqlc.arg('agent_grn')
  AND revision = sqlc.arg('expected_revision')
RETURNING *;

-- name: RenameAgentDefinition :one
UPDATE agent_definitions SET name = sqlc.arg('name')
WHERE agent_grn = sqlc.arg('agent_grn')
RETURNING *;

-- name: ListAgentDefinitionsByOrg :many
SELECT * FROM agent_definitions WHERE org_grn = $1 ORDER BY agent_grn;

-- The claim state is the assignment the claim wrote: an unclaimed
-- definition has none, and a claimed one binds the operator it names.
-- name: ListAgentDefinitionsByOperator :many
SELECT d.*, a.epoch AS claim_epoch, a.created_at AS claim_created_at, a.updated_at AS claim_updated_at
FROM agent_definitions d
LEFT JOIN agent_assignments a ON a.agent_grn = d.agent_grn
WHERE d.operator_grn = $1
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
