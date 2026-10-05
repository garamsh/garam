-- The narrower checks return NOT VALID: a grant or an authority that
-- already names agent:cutover is kept, and only new writes are held to
-- them.
ALTER TABLE operation_authorities
    DROP CONSTRAINT IF EXISTS operation_authorities_operation,
    DROP CONSTRAINT IF EXISTS operation_authorities_assignment,
    ADD CONSTRAINT operation_authorities_operation
        CHECK (operation IN ('agent:create', 'agent:configure', 'agent:recover',
                             'agent-template:read', 'agent-template:publish',
                             'execution-profile:read', 'agent:execution-read')) NOT VALID,
    ADD CONSTRAINT operation_authorities_assignment
        CHECK ((operation IN ('agent:configure', 'agent:recover', 'agent:execution-read')) = (assignee_grn IS NOT NULL)
           AND (assignee_grn IS NULL) = (assignment_epoch IS NULL)) NOT VALID;

ALTER TABLE operator_delegations
    DROP CONSTRAINT IF EXISTS operator_delegations_operations,
    ADD CONSTRAINT operator_delegations_operations
        CHECK (cardinality(operations) BETWEEN 1 AND 8
           AND operations <@ ARRAY['agent:create', 'agent:configure', 'agent:activate', 'agent:recover',
                                   'agent-template:read', 'agent-template:publish',
                                   'execution-profile:read', 'agent:execution-read']) NOT VALID;

DROP TABLE IF EXISTS agent_cutovers;
