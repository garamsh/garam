-- The narrower checks return NOT VALID: a grant or an authority that
-- already names a read or publication is kept, and only new writes are
-- held to them.
ALTER TABLE operation_authorities
    DROP CONSTRAINT IF EXISTS operation_authorities_request_target,
    DROP CONSTRAINT IF EXISTS operation_authorities_operation,
    DROP CONSTRAINT IF EXISTS operation_authorities_assignment,
    DROP COLUMN IF EXISTS request_target,
    ADD CONSTRAINT operation_authorities_operation
        CHECK (operation IN ('agent:create', 'agent:configure', 'agent:recover')) NOT VALID,
    ADD CONSTRAINT operation_authorities_assignment
        CHECK ((operation IN ('agent:configure', 'agent:recover')) = (assignee_grn IS NOT NULL)
           AND (assignee_grn IS NULL) = (assignment_epoch IS NULL)) NOT VALID;

ALTER TABLE operator_delegations
    DROP CONSTRAINT IF EXISTS operator_delegations_operations,
    ADD CONSTRAINT operator_delegations_operations
        CHECK (cardinality(operations) BETWEEN 1 AND 4
           AND operations <@ ARRAY['agent:create', 'agent:configure', 'agent:activate', 'agent:recover']) NOT VALID;

ALTER TABLE operators
    DROP CONSTRAINT IF EXISTS operators_control_endpoint,
    DROP COLUMN IF EXISTS control_endpoint;
