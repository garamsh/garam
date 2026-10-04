-- The narrower checks return NOT VALID: a grant or an authority that
-- already names agent:recover is kept, and only new writes are held to
-- them.
ALTER TABLE operation_authorities
    DROP CONSTRAINT IF EXISTS operation_authorities_operation,
    DROP CONSTRAINT IF EXISTS operation_authorities_assignment,
    ADD CONSTRAINT operation_authorities_operation
        CHECK (operation IN ('agent:create', 'agent:configure')) NOT VALID,
    ADD CONSTRAINT operation_authorities_assignment
        CHECK ((operation = 'agent:configure') = (assignee_grn IS NOT NULL)
           AND (assignee_grn IS NULL) = (assignment_epoch IS NULL)) NOT VALID;

ALTER TABLE operator_delegations
    DROP CONSTRAINT IF EXISTS operator_delegations_operations,
    ADD CONSTRAINT operator_delegations_operations
        CHECK (cardinality(operations) BETWEEN 1 AND 3
           AND operations <@ ARRAY['agent:create', 'agent:configure', 'agent:activate']) NOT VALID;

DROP FUNCTION IF EXISTS agent_execution_admit(text, bytea, text, timestamptz, timestamptz);
DROP FUNCTION IF EXISTS agent_credential_current(text, text, timestamptz, timestamptz);
DROP TABLE IF EXISTS agent_recoveries;
DROP TABLE IF EXISTS agent_credentials;
ALTER TABLE IF EXISTS agent_executions
    DROP CONSTRAINT IF EXISTS agent_executions_activation_fk,
    DROP CONSTRAINT IF EXISTS agent_executions_latest_activation_fk;
DROP TABLE IF EXISTS agent_activations;
DROP TABLE IF EXISTS agent_executions;
