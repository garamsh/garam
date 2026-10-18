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

-- ShareAgentExecution holds the agent's execution row FOR SHARE until
-- the transaction ends: a cutover switch, which takes it FOR UPDATE,
-- either commits before it or waits for the transaction to end.
-- name: ShareAgentExecution :one
SELECT mode FROM agent_executions WHERE agent_grn = $1 FOR SHARE;

-- IsAgentCutOver is a statement of its own after ShareAgentExecution,
-- so its snapshot is taken once that lock is granted.
-- name: IsAgentCutOver :one
SELECT EXISTS (
    SELECT 1 FROM agent_cutovers WHERE agent_grn = $1 AND stage = 'switched'
);

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

-- A definition is read with the stage of its GRN's open cutover
-- attempt, if any: a frozen or switched definition is archival, its
-- values preserved and no longer the source (ADR-0086). Nothing here
-- writes one: garam accepts no new definition, and an existing one
-- keeps its operator and values (ADR-0090, migration 000035); its
-- name is what RenameAgentDefinition changes alone.
-- name: GetAgentDefinition :one
SELECT d.*, c.stage AS cutover_stage
FROM agent_definitions d
LEFT JOIN agent_cutovers c ON c.agent_grn = d.agent_grn AND c.stage <> 'rolled_back'
WHERE d.agent_grn = $1;

-- name: RenameAgentDefinition :one
WITH renamed AS (
    UPDATE agent_definitions SET name = sqlc.arg('name')
    WHERE agent_definitions.agent_grn = sqlc.arg('agent_grn')
    RETURNING *
)
SELECT d.*, c.stage AS cutover_stage
FROM renamed d
LEFT JOIN agent_cutovers c ON c.agent_grn = d.agent_grn AND c.stage <> 'rolled_back';

-- name: ListAgentDefinitionsByOrg :many
SELECT d.*, c.stage AS cutover_stage
FROM agent_definitions d
LEFT JOIN agent_cutovers c ON c.agent_grn = d.agent_grn AND c.stage <> 'rolled_back'
WHERE d.org_grn = $1
ORDER BY d.agent_grn;

-- The claim state is the assignment the claim wrote: an unclaimed
-- definition has none, and a claimed one binds the operator it names.
-- An agent whose execution is fenced, or whose cutover is frozen or
-- switched, is moving or has moved to the Operator control service,
-- and its definition is no longer the operator's source (ADR-0077,
-- ADR-0086), so it is not listed.
-- name: ListAgentDefinitionsByOperator :many
SELECT d.*, a.epoch AS claim_epoch, a.created_at AS claim_created_at, a.updated_at AS claim_updated_at
FROM agent_definitions d
LEFT JOIN agent_assignments a ON a.agent_grn = d.agent_grn
LEFT JOIN agent_executions e ON e.agent_grn = d.agent_grn
WHERE d.operator_grn = $1 AND (e.mode IS NULL OR e.mode = 'legacy')
  AND NOT EXISTS (
      SELECT 1 FROM agent_cutovers c
      WHERE c.agent_grn = d.agent_grn AND c.stage <> 'rolled_back'
  )
ORDER BY d.agent_grn;

-- A claim holds its organization's row before the definition's, with
-- the KEY SHARE lock the agent insert's foreign key takes anyway: an
-- organization's deletion holds that row before its cascade reaches
-- the definition, so the two take them in one order (#1441).
-- name: HoldAgentOrg :one
SELECT grn FROM orgs WHERE grn = $1 FOR KEY SHARE;

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

-- name: GetAgentTemplate :one
SELECT * FROM agent_templates WHERE grn = $1;

-- name: ListAgentTemplatesByOrg :many
SELECT * FROM agent_templates WHERE org_grn = $1 ORDER BY name, grn;

-- AdmitExecution runs the agent execution fence (ADR-0080) as the
-- first statement of a protected write's transaction; the row it locks
-- is held until that transaction ends.
-- name: AdmitExecution :one
SELECT agent_execution_admit(
    sqlc.arg('agent_grn'), sqlc.narg('token_hash')::bytea, sqlc.arg('serial'),
    sqlc.arg('not_before')::timestamptz, sqlc.arg('not_after')::timestamptz
)::text;

-- The execution a token is minted against, held FOR SHARE so a
-- recovery or a cutover switch committing first is seen and one
-- committing after waits for the mint (ADR-0100). The assignment and
-- the cutover are read by statements of their own after it, so their
-- snapshot is taken once this lock is granted.
-- name: ShareAgentLineage :one
SELECT mode, lineage FROM agent_executions WHERE agent_grn = $1 FOR SHARE;

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
