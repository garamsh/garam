-- agent:replace is delegated and handed over as agent:recover is
-- (ADR-0107): it targets an existing agent, records where the agent
-- runs now and binds one destination, the controller a managed
-- replacement moves the agent to. A grant now names up to eleven
-- operations, one per operation that exists.
--
-- Every check this migration (re)adds on those two tables is NOT
-- VALID, as 000044's are: a new write is held to it, and a row a
-- rollback of this migration kept does not stop it applying again.
ALTER TABLE operator_delegations
    DROP CONSTRAINT operator_delegations_operations,
    ADD CONSTRAINT operator_delegations_operations
        CHECK (cardinality(operations) BETWEEN 1 AND 11
           AND operations <@ ARRAY['agent:create', 'agent:configure', 'agent:activate', 'agent:recover',
                                   'agent-template:read', 'agent-template:publish',
                                   'execution-profile:read', 'agent:execution-read',
                                   'agent:cutover', 'operator:read', 'agent:replace']) NOT VALID;

ALTER TABLE operation_authorities
    ADD COLUMN destination_grn text,
    DROP CONSTRAINT operation_authorities_operation,
    DROP CONSTRAINT operation_authorities_assignment,
    ADD CONSTRAINT operation_authorities_operation
        CHECK (operation IN ('agent:create', 'agent:configure', 'agent:recover',
                             'agent-template:read', 'agent-template:publish',
                             'execution-profile:read', 'agent:execution-read',
                             'agent:cutover', 'operator:read', 'agent:replace')) NOT VALID,
    ADD CONSTRAINT operation_authorities_assignment
        CHECK ((operation IN ('agent:configure', 'agent:recover', 'agent:execution-read', 'agent:cutover',
                              'agent:replace'))
                = (assignee_grn IS NOT NULL)
           AND (assignee_grn IS NULL) = (assignment_epoch IS NULL)) NOT VALID,
    ADD CONSTRAINT operation_authorities_destination
        CHECK ((operation = 'agent:replace') = (destination_grn IS NOT NULL)) NOT VALID;

-- A replacement approval names the agent it moves and the assignment
-- it expects, and, once admission spent it, the epoch and lineage the
-- move wrote. It records no managed creation: the agent keeps the one
-- that created it, if any.
ALTER TABLE agent_enrollment_approvals
    ADD COLUMN replaced_agent_grn    text REFERENCES agent_executions (agent_grn) ON DELETE CASCADE,
    ADD COLUMN expected_assignee_grn text,
    ADD COLUMN expected_epoch        integer CHECK (expected_epoch > 0),
    ADD COLUMN admitted_epoch        integer CHECK (admitted_epoch > 0),
    ADD COLUMN admitted_lineage      uuid,
    DROP CONSTRAINT agent_enrollment_approvals_admission_is_whole,
    ADD CONSTRAINT agent_enrollment_approvals_replacement_is_whole
        CHECK ((replaced_agent_grn IS NULL) = (expected_assignee_grn IS NULL)
           AND (replaced_agent_grn IS NULL) = (expected_epoch IS NULL)),
    ADD CONSTRAINT agent_enrollment_approvals_admission_is_whole
        CHECK ((admitted_epoch IS NULL) = (admitted_lineage IS NULL)
           AND (replaced_agent_grn IS NULL OR agent_grn IS NULL)
           AND (replaced_agent_grn IS NOT NULL OR admitted_epoch IS NULL)
           AND (admitted_at IS NULL) = (agent_grn IS NULL AND admitted_epoch IS NULL));
