DROP TABLE IF EXISTS agent_supervisor_renewals;
DROP TABLE IF EXISTS agent_supervisors;
DROP TABLE IF EXISTS agent_enrollments;

-- The operator-only check returns NOT VALID: an assignment a supervisor
-- already holds is kept, and only new writes are held to it.
ALTER TABLE agent_assignments
    DROP CONSTRAINT IF EXISTS agent_assignments_assignee_service_type,
    DROP CONSTRAINT IF EXISTS agent_assignments_same_org;
ALTER TABLE agent_assignments RENAME COLUMN assignee_grn TO operator_grn;
ALTER TABLE agent_assignments
    ADD CONSTRAINT agent_assignments_operator_service_type
        CHECK (split_part(operator_grn, ':', 4) = 'operator') NOT VALID,
    ADD CONSTRAINT agent_assignments_same_org
        CHECK (split_part(operator_grn, ':', 2) = split_part(agent_grn, ':', 2));
