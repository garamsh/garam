-- A replacement approval has no shape below this migration, so it goes;
-- the moves it admitted stay, as any reassignment does. The narrower
-- checks return NOT VALID: a grant or an authority that already names
-- agent:replace is kept, and only new writes are held to them.
DELETE FROM agent_enrollment_approvals WHERE replaced_agent_grn IS NOT NULL;

ALTER TABLE agent_enrollment_approvals
    DROP CONSTRAINT IF EXISTS agent_enrollment_approvals_admission_is_whole,
    DROP CONSTRAINT IF EXISTS agent_enrollment_approvals_replacement_is_whole,
    DROP COLUMN IF EXISTS admitted_lineage,
    DROP COLUMN IF EXISTS admitted_epoch,
    DROP COLUMN IF EXISTS expected_epoch,
    DROP COLUMN IF EXISTS expected_assignee_grn,
    DROP COLUMN IF EXISTS replaced_agent_grn,
    ADD CONSTRAINT agent_enrollment_approvals_admission_is_whole
        CHECK ((agent_grn IS NULL) = (admitted_at IS NULL));

ALTER TABLE operation_authorities
    DROP CONSTRAINT IF EXISTS operation_authorities_destination,
    DROP CONSTRAINT IF EXISTS operation_authorities_assignment,
    DROP CONSTRAINT IF EXISTS operation_authorities_operation,
    DROP COLUMN IF EXISTS destination_grn,
    ADD CONSTRAINT operation_authorities_operation
        CHECK (operation IN ('agent:create', 'agent:configure', 'agent:recover',
                             'agent-template:read', 'agent-template:publish',
                             'execution-profile:read', 'agent:execution-read',
                             'agent:cutover', 'operator:read')) NOT VALID,
    ADD CONSTRAINT operation_authorities_assignment
        CHECK ((operation IN ('agent:configure', 'agent:recover', 'agent:execution-read', 'agent:cutover'))
                = (assignee_grn IS NOT NULL)
           AND (assignee_grn IS NULL) = (assignment_epoch IS NULL)) NOT VALID;

ALTER TABLE operator_delegations
    DROP CONSTRAINT IF EXISTS operator_delegations_operations,
    ADD CONSTRAINT operator_delegations_operations
        CHECK (cardinality(operations) BETWEEN 1 AND 10
           AND operations <@ ARRAY['agent:create', 'agent:configure', 'agent:activate', 'agent:recover',
                                   'agent-template:read', 'agent-template:publish',
                                   'execution-profile:read', 'agent:execution-read',
                                   'agent:cutover', 'operator:read']) NOT VALID;
