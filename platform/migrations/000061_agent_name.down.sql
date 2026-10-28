ALTER TABLE agent_enrollment_approvals
    DROP CONSTRAINT agent_enrollment_approvals_replacement_keeps_name,
    DROP COLUMN name;
ALTER TABLE agent_managed_creations DROP COLUMN name;
ALTER TABLE agents DROP COLUMN name;
