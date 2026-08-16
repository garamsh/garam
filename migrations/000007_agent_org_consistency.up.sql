-- An agent's GRN carries its organization in the organization segment
-- (ADR-0001) and ADR-0014 rests tenancy on that segment, so it must
-- agree with the org the row belongs to. internal/agent refuses the
-- pairing as well; this is the copy no call path and no direct statement
-- routes around.
ALTER TABLE agents
    ADD CONSTRAINT agents_grn_same_org
    CHECK (split_part(grn, ':', 2) = split_part(org_grn, ':', 5));

-- An operator is registered nowhere, so no foreign key says the GRN
-- names one and a typo would otherwise be a valid operator.
ALTER TABLE agent_assignments
    ADD CONSTRAINT agent_assignments_operator_service_type
    CHECK (split_part(operator_grn, ':', 4) = 'operator');

-- An assignment stays inside one org (ADR-0015): an operator holding one
-- org's CA-signed certificate cannot be assigned another org's agent.
-- Both GRNs name their org directly, so the row decides this alone,
-- where a policy attachment has to read the policy's org from another
-- table. An operator is addressed as an agent is (ADR-0012), so it is
-- always org-owned and no root-owned identity is admitted here.
ALTER TABLE agent_assignments
    ADD CONSTRAINT agent_assignments_same_org
    CHECK (split_part(operator_grn, ':', 2) = split_part(agent_grn, ':', 2));
