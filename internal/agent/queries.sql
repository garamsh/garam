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
