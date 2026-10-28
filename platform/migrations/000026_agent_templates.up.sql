-- What an organization calls an agent it defined, and the templates it
-- starts a definition from (ADR-0075).
--
-- The name is the organization's word for the agent and nothing else:
-- the GRN stays the agent's identity, the subject its certificate
-- names and the key every other table joins on, so renaming touches no
-- assignment, credential or membership. A definition written before
-- this migration carries no name, so it is given its identifier, which
-- is what the console showed it by until now.
ALTER TABLE agent_definitions ADD COLUMN name text;

UPDATE agent_definitions SET name = split_part(agent_grn, ':', 5);

-- internal/agent refuses the bound too; this is the copy no call path
-- routes around, as 000010 says of its own checks.
ALTER TABLE agent_definitions
    ALTER COLUMN name SET NOT NULL,
    ADD CONSTRAINT agent_definitions_name_length
        CHECK (btrim(name) <> '' AND char_length(name) <= 100);

-- A template is a starting point an organization keeps for defining
-- agents: a name, what it is for, and values a definition may copy.
-- Copying happens once, when a definition is written, so no column
-- here or in `agent_definitions` links the two: editing or deleting a
-- template can reach no agent defined from it. It names no operator
-- and no agent; which operator runs an agent is chosen per definition.
CREATE TABLE agent_templates (
    grn          text PRIMARY KEY,
    org_grn      text NOT NULL REFERENCES orgs (grn) ON DELETE CASCADE,
    name         text NOT NULL,
    description  text NOT NULL DEFAULT '',
    agent_values jsonb NOT NULL,
    created_by   text NOT NULL,
    created_at   timestamptz NOT NULL,
    updated_at   timestamptz NOT NULL,

    -- The GRN must name a template of the org the row belongs to, as
    -- `agent_definitions` requires of the agent it mints (000010).
    CONSTRAINT agent_templates_grn_ownership
        CHECK (split_part(grn, ':', 4) = 'agent-template'
           AND split_part(grn, ':', 2) = split_part(org_grn, ':', 5)),
    CONSTRAINT agent_templates_name_length
        CHECK (btrim(name) <> '' AND char_length(name) <= 100),
    CONSTRAINT agent_templates_description_length
        CHECK (char_length(description) <= 1000),
    CONSTRAINT agent_templates_values_object
        CHECK (jsonb_typeof(agent_values) = 'object')
);

CREATE INDEX agent_templates_org_idx ON agent_templates (org_grn);
