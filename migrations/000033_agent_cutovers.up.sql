-- A legacy agent moves its desired-state source to the Operator
-- control service one GRN at a time, through stages garam records
-- under the agent's execution lock (ADR-0086, agent-cutover.v1).
--
-- One row per attempt, keyed by the Operator's import identifier, so a
-- retried stage answers what it recorded and a rolled-back attempt's
-- late replay is told apart from a new one. At most one attempt per
-- agent is open, frozen or switched; a rolled-back one stays as the
-- record that refuses its replay.
--
-- An attempt names the execution row rather than the definition: an
-- agent with no definition cuts over too, with an empty source. The
-- definition and its values are never written or deleted here, so the
-- source stays preserved and read-only after the switch.
CREATE TABLE agent_cutovers (
    agent_grn     text NOT NULL REFERENCES agent_executions (agent_grn) ON DELETE CASCADE,
    import_id     text NOT NULL CHECK (import_id ~ '^[A-Za-z0-9._~-]{1,128}$'),
    stage         text NOT NULL CHECK (stage IN ('frozen', 'switched', 'rolled_back')),
    -- SHA-256 of the RFC 8785 JSON of the source as frozen, the digest
    -- the Operator's import was verified against.
    frozen_digest bytea NOT NULL CHECK (octet_length(frozen_digest) = 32),
    -- The authority the freeze was admitted under: the caller, its
    -- grant and the agent:cutover reference.
    audience_grn  text NOT NULL,
    grant_id      text NOT NULL,
    operation_ref text NOT NULL,
    created_at    timestamptz NOT NULL,
    switched_at   timestamptz,
    ended_at      timestamptz,
    PRIMARY KEY (agent_grn, import_id),

    CONSTRAINT agent_cutovers_switch_is_whole
        CHECK ((stage = 'switched') = (switched_at IS NOT NULL)),
    CONSTRAINT agent_cutovers_end_is_whole
        CHECK ((stage = 'rolled_back') = (ended_at IS NOT NULL))
);

CREATE UNIQUE INDEX agent_cutovers_one_open ON agent_cutovers (agent_grn)
    WHERE stage <> 'rolled_back';

-- agent:cutover is delegated and handed over as agent:configure is: it
-- acts on an existing agent, so its authority records where the agent
-- runs when it is minted, and it binds no request target.
ALTER TABLE operator_delegations
    DROP CONSTRAINT operator_delegations_operations,
    ADD CONSTRAINT operator_delegations_operations
        CHECK (cardinality(operations) BETWEEN 1 AND 9
           AND operations <@ ARRAY['agent:create', 'agent:configure', 'agent:activate', 'agent:recover',
                                   'agent-template:read', 'agent-template:publish',
                                   'execution-profile:read', 'agent:execution-read',
                                   'agent:cutover']);

ALTER TABLE operation_authorities
    DROP CONSTRAINT operation_authorities_operation,
    DROP CONSTRAINT operation_authorities_assignment,
    ADD CONSTRAINT operation_authorities_operation
        CHECK (operation IN ('agent:create', 'agent:configure', 'agent:recover',
                             'agent-template:read', 'agent-template:publish',
                             'execution-profile:read', 'agent:execution-read',
                             'agent:cutover')),
    ADD CONSTRAINT operation_authorities_assignment
        CHECK ((operation IN ('agent:configure', 'agent:recover', 'agent:execution-read', 'agent:cutover'))
                = (assignee_grn IS NOT NULL)
           AND (assignee_grn IS NULL) = (assignment_epoch IS NULL));
