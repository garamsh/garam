-- An agent is an actor an operator provisions from outside garam
-- (ADR-0012). It is never an organization member, so nothing here
-- references users. Provisioning state is what the operator reports
-- about the pod; runtime state is what the agent reports about itself.
CREATE TABLE agents (
    grn                text PRIMARY KEY,
    org_grn            text NOT NULL REFERENCES orgs (grn) ON DELETE CASCADE,
    provisioning_state text NOT NULL CHECK (provisioning_state IN ('pending', 'provisioned', 'ready', 'failed')),
    runtime_state      text NOT NULL CHECK (runtime_state IN ('unknown', 'connected', 'disconnected')),
    created_at         timestamptz NOT NULL,
    updated_at         timestamptz NOT NULL
);

CREATE INDEX agents_org_idx ON agents (org_grn);

-- Assignment binds one agent to one operator and is not part of the
-- agent's identity (ADR-0012), so the agent is the primary key and a
-- reassignment moves the operator on the row it already has. The epoch
-- rises with every assignment and fences the operator it replaced: a
-- state report carrying an earlier epoch matches no row. An operator is
-- addressed by GRN and registered nowhere yet, so the column carries no
-- foreign key.
CREATE TABLE agent_assignments (
    agent_grn    text PRIMARY KEY REFERENCES agents (grn) ON DELETE CASCADE,
    operator_grn text NOT NULL,
    epoch        integer NOT NULL CHECK (epoch > 0),
    created_at   timestamptz NOT NULL,
    updated_at   timestamptz NOT NULL
);
