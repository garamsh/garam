-- The narrower checks return NOT VALID: a grant or an authority that
-- already names operator:read is kept, and only new writes are held to
-- them.
ALTER TABLE operation_authorities
    DROP CONSTRAINT IF EXISTS operation_authorities_operation,
    DROP CONSTRAINT IF EXISTS operation_authorities_request_target,
    ADD CONSTRAINT operation_authorities_operation
        CHECK (operation IN ('agent:create', 'agent:configure', 'agent:recover',
                             'agent-template:read', 'agent-template:publish',
                             'execution-profile:read', 'agent:execution-read',
                             'agent:cutover')) NOT VALID,
    ADD CONSTRAINT operation_authorities_request_target
        CHECK ((operation IN ('agent-template:read', 'agent-template:publish',
                              'execution-profile:read', 'agent:execution-read',
                              'agent:cutover')) = (request_target IS NOT NULL)
           AND (request_target IS NULL OR char_length(request_target) BETWEEN 1 AND 2048)) NOT VALID;

ALTER TABLE operator_delegations
    DROP CONSTRAINT IF EXISTS operator_delegations_operations,
    ADD CONSTRAINT operator_delegations_operations
        CHECK (cardinality(operations) BETWEEN 1 AND 9
           AND operations <@ ARRAY['agent:create', 'agent:configure', 'agent:activate', 'agent:recover',
                                   'agent-template:read', 'agent-template:publish',
                                   'execution-profile:read', 'agent:execution-read',
                                   'agent:cutover']) NOT VALID;
