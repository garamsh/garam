-- Rolling back below 000033 drops the record of every cutover. A
-- frozen or switched attempt is what keeps its agent out of its
-- operator's poll and off the legacy certificate route, so dropping one
-- would give the agent two desired sources. The rollback refuses while
-- any is held: below 000033 after a switch, only the reverse migration
-- restores garam as the source (ADR-0086). Rolled-back attempts hold
-- nothing and are dropped.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM agent_cutovers WHERE stage <> 'rolled_back') THEN
        RAISE EXCEPTION 'agent_cutovers holds a frozen or switched attempt: rolling back below 000033 requires the reverse migration (ADR-0086)';
    END IF;
END
$$;

-- The narrower checks return NOT VALID: a grant or an authority that
-- already names agent:cutover is kept, and only new writes are held to
-- them.
ALTER TABLE operation_authorities
    DROP CONSTRAINT IF EXISTS operation_authorities_operation,
    DROP CONSTRAINT IF EXISTS operation_authorities_assignment,
    DROP CONSTRAINT IF EXISTS operation_authorities_request_target,
    ADD CONSTRAINT operation_authorities_operation
        CHECK (operation IN ('agent:create', 'agent:configure', 'agent:recover',
                             'agent-template:read', 'agent-template:publish',
                             'execution-profile:read', 'agent:execution-read')) NOT VALID,
    ADD CONSTRAINT operation_authorities_assignment
        CHECK ((operation IN ('agent:configure', 'agent:recover', 'agent:execution-read')) = (assignee_grn IS NOT NULL)
           AND (assignee_grn IS NULL) = (assignment_epoch IS NULL)) NOT VALID,
    ADD CONSTRAINT operation_authorities_request_target
        CHECK ((operation IN ('agent-template:read', 'agent-template:publish',
                              'execution-profile:read', 'agent:execution-read')) = (request_target IS NOT NULL)
           AND (request_target IS NULL OR char_length(request_target) BETWEEN 1 AND 2048)) NOT VALID;

ALTER TABLE operator_delegations
    DROP CONSTRAINT IF EXISTS operator_delegations_operations,
    ADD CONSTRAINT operator_delegations_operations
        CHECK (cardinality(operations) BETWEEN 1 AND 8
           AND operations <@ ARRAY['agent:create', 'agent:configure', 'agent:activate', 'agent:recover',
                                   'agent-template:read', 'agent-template:publish',
                                   'execution-profile:read', 'agent:execution-read']) NOT VALID;

DROP TABLE IF EXISTS agent_cutovers;
