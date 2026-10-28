-- A managed agent has a display name in garam, held on the agent
-- record (ADR-0119). It is optional, trimmed and 1-100 characters, the
-- bound agent.NameMaxLength states; an agent without one is shown by
-- its identifier.
--
-- A managed creation and an agent-enrollment.v1 approval keep the name
-- they were asked for, so a retry is the same request only when it asks
-- for the same name, whatever a rename has made of the agent since.
ALTER TABLE agents
    ADD COLUMN name text CHECK (name IS NULL OR char_length(name) BETWEEN 1 AND 100);

ALTER TABLE agent_managed_creations
    ADD COLUMN name text CHECK (name IS NULL OR char_length(name) BETWEEN 1 AND 100);

ALTER TABLE agent_enrollment_approvals
    ADD COLUMN name text CHECK (name IS NULL OR char_length(name) BETWEEN 1 AND 100),
    ADD CONSTRAINT agent_enrollment_approvals_replacement_keeps_name
        CHECK (name IS NULL OR replaced_agent_grn IS NULL);
