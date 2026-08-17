-- A definition is what an organization says an agent should be: which
-- operator is to run it, and the values that operator constructs it
-- from (ADR-0026). It is a record and not a command, so a row here
-- says nothing about whether an agent exists: an operator's report
-- writes `agents`, and this table is written by the console alone.
-- Separate tables is what keeps the writers apart — `agents` already
-- carries what the operator reports about the pod and what the agent
-- reports about itself, and intent would be a third writer on it.
--
-- The identifier is the GRN garam mints for the agent the definition
-- describes, so `agents` holds no row under it until an operator
-- reports one and no foreign key here may say otherwise.
CREATE TABLE agent_definitions (
    agent_grn    text PRIMARY KEY,
    org_grn      text NOT NULL REFERENCES orgs (grn) ON DELETE CASCADE,
    operator_grn text NOT NULL,
    -- `values` is a reserved word in SQL, so the column carries the
    -- noun the table already names.
    agent_values jsonb NOT NULL,
    defined_by   text NOT NULL,
    created_at   timestamptz NOT NULL,

    -- The agent GRN must name an agent of the org the row belongs to,
    -- as `agents` requires of its own (000007). internal/agent refuses
    -- both halves too; this is the copy no call path routes around.
    CONSTRAINT agent_definitions_grn_ownership
        CHECK (split_part(agent_grn, ':', 4) = 'agent'
           AND split_part(agent_grn, ':', 2) = split_part(org_grn, ':', 5)),
    -- An operator is registered nowhere, so no foreign key says the GRN
    -- names one and a typo would otherwise be a valid operator.
    CONSTRAINT agent_definitions_operator_service_type
        CHECK (split_part(operator_grn, ':', 4) = 'operator'),
    -- A definition stays inside one org, as an assignment does
    -- (ADR-0015): an operator holds one org's certificate and cannot be
    -- named by another org's definition.
    CONSTRAINT agent_definitions_same_org
        CHECK (split_part(operator_grn, ':', 2) = split_part(agent_grn, ':', 2)),
    -- jsonb admits an array or a scalar as readily as an object, and a
    -- stored one would fail on the way back out instead of on the way
    -- in.
    CONSTRAINT agent_definitions_values_object
        CHECK (jsonb_typeof(agent_values) = 'object')
);

CREATE INDEX agent_definitions_org_idx ON agent_definitions (org_grn);
