-- garam keeps no legacy execution mode, staged cut-over or agent
-- definition (ADR-0118, which runs ADR-0117 item 5 now). The legacy
-- agents go with their definitions, and ON DELETE CASCADE takes each
-- one's execution, assignment, tokens, lineage and cut-over rows;
-- messages name their recipients by GRN alone and stay. The PM holds a
-- private pg_dump of the one deployment's rows taken before this ran.
--
-- It deletes legacy rows only. A definition or a cut-over attempt of an
-- agent already fenced, or a delegation grant that names agent:cutover
-- and nothing else, is refused here, naming what it found, rather than
-- deleted with the rest.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM agent_definitions d
               JOIN agent_executions e ON e.agent_grn = d.agent_grn
               WHERE e.mode <> 'legacy') THEN
        RAISE EXCEPTION 'agent_definitions names a fenced agent: garam deletes legacy agents only (ADR-0118)';
    END IF;
    IF EXISTS (SELECT 1 FROM agent_cutovers c
               JOIN agent_executions e ON e.agent_grn = c.agent_grn
               WHERE e.mode <> 'legacy') THEN
        RAISE EXCEPTION 'agent_cutovers holds an attempt of a fenced agent: garam deletes legacy agents only (ADR-0118)';
    END IF;
    IF EXISTS (SELECT 1 FROM operator_delegations WHERE operations = ARRAY['agent:cutover']) THEN
        RAISE EXCEPTION 'operator_delegations holds a grant of agent:cutover alone: garam keeps no cut-over (ADR-0118)';
    END IF;
END
$$;

DROP TABLE agent_definitions;
DROP FUNCTION agent_definitions_closed();

DELETE FROM agents
WHERE grn IN (SELECT agent_grn FROM agent_executions WHERE mode = 'legacy');

DROP TABLE agent_cutovers;

-- agent:cutover authorized only the cut-over routes, so a grant loses
-- it and an authority minted for it goes. The checks return NOT VALID,
-- as 000051's are.
UPDATE operator_delegations
SET operations = array_remove(operations, 'agent:cutover')
WHERE 'agent:cutover' = ANY (operations);

DELETE FROM operation_authorities WHERE operation = 'agent:cutover';

ALTER TABLE operator_delegations
    DROP CONSTRAINT operator_delegations_operations,
    ADD CONSTRAINT operator_delegations_operations
        CHECK (cardinality(operations) BETWEEN 1 AND 10
           AND operations <@ ARRAY['agent:create', 'agent:configure', 'agent:activate', 'agent:recover',
                                   'agent-template:read', 'agent-template:publish',
                                   'execution-profile:read', 'agent:execution-read',
                                   'operator:read', 'agent:replace']) NOT VALID;

ALTER TABLE operation_authorities
    DROP CONSTRAINT operation_authorities_operation,
    DROP CONSTRAINT operation_authorities_assignment,
    DROP CONSTRAINT operation_authorities_request_target,
    ADD CONSTRAINT operation_authorities_operation
        CHECK (operation IN ('agent:create', 'agent:configure', 'agent:recover',
                             'agent-template:read', 'agent-template:publish',
                             'execution-profile:read', 'agent:execution-read',
                             'operator:read', 'agent:replace')) NOT VALID,
    ADD CONSTRAINT operation_authorities_assignment
        CHECK ((operation IN ('agent:configure', 'agent:recover', 'agent:execution-read', 'agent:replace'))
                = (assignee_grn IS NOT NULL)
           AND (assignee_grn IS NULL) = (assignment_epoch IS NULL)) NOT VALID,
    ADD CONSTRAINT operation_authorities_request_target
        CHECK ((operation IN ('agent-template:read', 'agent-template:publish',
                              'execution-profile:read', 'agent:execution-read',
                              'operator:read')) = (request_target IS NOT NULL)
           AND (request_target IS NULL OR char_length(request_target) BETWEEN 1 AND 2048)) NOT VALID;

-- Every execution left is fenced, so the mode goes, and admission
-- requires the activation's token of every agent.
CREATE OR REPLACE FUNCTION agent_execution_admit(
    p_agent_grn text, p_token_hash bytea, p_serial text,
    p_not_before timestamptz, p_not_after timestamptz
) RETURNS text
LANGUAGE plpgsql VOLATILE AS $$
DECLARE
    v_token_hash bytea;
BEGIN
    SELECT token_hash INTO v_token_hash
    FROM agent_executions WHERE agent_grn = p_agent_grn
    FOR SHARE;
    IF NOT FOUND THEN
        RETURN 'unknown_agent';
    END IF;
    IF p_token_hash IS NULL THEN
        RETURN 'token_required';
    ELSIF v_token_hash IS NULL OR v_token_hash <> p_token_hash THEN
        RETURN 'token_stale';
    END IF;
    IF NOT agent_credential_current(p_agent_grn, p_serial, p_not_before, p_not_after) THEN
        RETURN 'credential_fenced';
    END IF;
    RETURN 'admitted';
END
$$;

ALTER TABLE agent_executions
    DROP CONSTRAINT agent_executions_only_fenced_activate,
    DROP COLUMN mode;
