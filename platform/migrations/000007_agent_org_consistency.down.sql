ALTER TABLE agent_assignments DROP CONSTRAINT IF EXISTS agent_assignments_same_org;
ALTER TABLE agent_assignments DROP CONSTRAINT IF EXISTS agent_assignments_operator_service_type;
ALTER TABLE agents DROP CONSTRAINT IF EXISTS agents_grn_ownership;
