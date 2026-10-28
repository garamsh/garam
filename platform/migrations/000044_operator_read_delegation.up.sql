-- operator:read is delegated and handed over as agent-template:read is
-- (#1374): it reads what the Operator reports for every operator of the
-- organization, targets the organization, names no controller and
-- binds the exact request target it is presented to. A grant now names
-- up to ten operations, one per operation that exists.
--
-- Every check this migration (re)adds is NOT VALID, as 000033's are: a
-- new write is held to it, and a row a rollback of this migration kept
-- does not stop it applying again.
ALTER TABLE operator_delegations
    DROP CONSTRAINT operator_delegations_operations,
    ADD CONSTRAINT operator_delegations_operations
        CHECK (cardinality(operations) BETWEEN 1 AND 10
           AND operations <@ ARRAY['agent:create', 'agent:configure', 'agent:activate', 'agent:recover',
                                   'agent-template:read', 'agent-template:publish',
                                   'execution-profile:read', 'agent:execution-read',
                                   'agent:cutover', 'operator:read']) NOT VALID;

ALTER TABLE operation_authorities
    DROP CONSTRAINT operation_authorities_operation,
    DROP CONSTRAINT operation_authorities_request_target,
    ADD CONSTRAINT operation_authorities_operation
        CHECK (operation IN ('agent:create', 'agent:configure', 'agent:recover',
                             'agent-template:read', 'agent-template:publish',
                             'execution-profile:read', 'agent:execution-read',
                             'agent:cutover', 'operator:read')) NOT VALID,
    ADD CONSTRAINT operation_authorities_request_target
        CHECK ((operation IN ('agent-template:read', 'agent-template:publish',
                              'execution-profile:read', 'agent:execution-read',
                              'agent:cutover', 'operator:read')) = (request_target IS NOT NULL)
           AND (request_target IS NULL OR char_length(request_target) BETWEEN 1 AND 2048)) NOT VALID;
