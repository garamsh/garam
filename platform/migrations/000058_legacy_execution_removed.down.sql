-- The schema 000058 removed comes back empty. Every execution it kept
-- is fenced, so the mode returns as 'fenced' for each, and the legacy
-- agents, definitions and cut-over attempts it deleted come back only
-- from the backup taken before it ran.
ALTER TABLE agent_executions
    ADD COLUMN mode text NOT NULL DEFAULT 'fenced' CHECK (mode IN ('legacy', 'fenced')),
    ADD CONSTRAINT agent_executions_only_fenced_activate
        CHECK (activation_id IS NULL OR mode = 'fenced');
ALTER TABLE agent_executions ALTER COLUMN mode SET DEFAULT 'legacy';

CREATE OR REPLACE FUNCTION agent_execution_admit(
    p_agent_grn text, p_token_hash bytea, p_serial text,
    p_not_before timestamptz, p_not_after timestamptz
) RETURNS text
LANGUAGE plpgsql VOLATILE AS $$
DECLARE
    v_mode text;
    v_token_hash bytea;
BEGIN
    SELECT mode, token_hash INTO v_mode, v_token_hash
    FROM agent_executions WHERE agent_grn = p_agent_grn
    FOR SHARE;
    IF NOT FOUND THEN
        RETURN 'unknown_agent';
    END IF;
    IF v_mode = 'legacy' THEN
        IF p_token_hash IS NOT NULL THEN
            RETURN 'not_cut_over';
        END IF;
    ELSIF p_token_hash IS NULL THEN
        RETURN 'token_required';
    ELSIF v_token_hash IS NULL OR v_token_hash <> p_token_hash THEN
        RETURN 'token_stale';
    END IF;
    IF NOT agent_credential_current(p_agent_grn, p_serial, p_not_before, p_not_after) THEN
        RETURN 'credential_fenced';
    END IF;
    RETURN 'admitted';
END
$$;

ALTER TABLE operation_authorities
    DROP CONSTRAINT operation_authorities_request_target,
    DROP CONSTRAINT operation_authorities_assignment,
    DROP CONSTRAINT operation_authorities_operation,
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
    ADD CONSTRAINT operation_authorities_request_target
        CHECK ((operation IN ('agent-template:read', 'agent-template:publish',
                              'execution-profile:read', 'agent:execution-read',
                              'agent:cutover', 'operator:read')) = (request_target IS NOT NULL)
           AND (request_target IS NULL OR char_length(request_target) BETWEEN 1 AND 2048)) NOT VALID;

ALTER TABLE operator_delegations
    DROP CONSTRAINT operator_delegations_operations,
    ADD CONSTRAINT operator_delegations_operations
        CHECK (cardinality(operations) BETWEEN 1 AND 11
           AND operations <@ ARRAY['agent:create', 'agent:configure', 'agent:activate', 'agent:recover',
                                   'agent-template:read', 'agent-template:publish',
                                   'execution-profile:read', 'agent:execution-read',
                                   'agent:cutover', 'operator:read', 'agent:replace']) NOT VALID;

CREATE TABLE agent_cutovers (
    agent_grn     text NOT NULL REFERENCES agent_executions (agent_grn) ON DELETE CASCADE,
    import_id     text NOT NULL CHECK (import_id ~ '^[A-Za-z0-9._~-]{1,128}$'),
    stage         text NOT NULL CHECK (stage IN ('frozen', 'switched', 'rolled_back')),
    frozen_digest bytea NOT NULL CHECK (octet_length(frozen_digest) = 32),
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

CREATE TABLE agent_definitions (
    agent_grn    text PRIMARY KEY,
    org_grn      text NOT NULL REFERENCES orgs (grn) ON DELETE CASCADE,
    operator_grn text NOT NULL,
    agent_values jsonb NOT NULL,
    defined_by   text NOT NULL,
    created_at   timestamptz NOT NULL,
    name         text NOT NULL,
    CONSTRAINT agent_definitions_grn_ownership
        CHECK (split_part(agent_grn, ':', 4) = 'agent'
           AND split_part(agent_grn, ':', 2) = split_part(org_grn, ':', 5)),
    CONSTRAINT agent_definitions_operator_service_type
        CHECK (split_part(operator_grn, ':', 4) = 'operator'),
    CONSTRAINT agent_definitions_same_org
        CHECK (split_part(operator_grn, ':', 2) = split_part(agent_grn, ':', 2)),
    CONSTRAINT agent_definitions_values_object
        CHECK (jsonb_typeof(agent_values) = 'object'),
    CONSTRAINT agent_definitions_name_length
        CHECK (btrim(name) <> '' AND char_length(name) <= 100),
    CONSTRAINT agent_definitions_operator_registered
        FOREIGN KEY (operator_grn) REFERENCES operators (grn) NOT VALID
);
CREATE INDEX agent_definitions_org_idx ON agent_definitions (org_grn);
CREATE INDEX agent_definitions_operator_idx ON agent_definitions (operator_grn);

CREATE FUNCTION agent_definitions_closed() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' AND NOT EXISTS (SELECT 1 FROM orgs WHERE grn = OLD.org_grn) THEN
        RETURN OLD;
    END IF;
    IF TG_OP = 'UPDATE'
       AND (NEW.agent_grn, NEW.org_grn, NEW.operator_grn, NEW.agent_values, NEW.defined_by, NEW.created_at)
           IS NOT DISTINCT FROM
           (OLD.agent_grn, OLD.org_grn, OLD.operator_grn, OLD.agent_values, OLD.defined_by, OLD.created_at) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'agent_definitions accepts no desired value';
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER agent_definitions_closed
    BEFORE INSERT OR UPDATE OR DELETE ON agent_definitions
    FOR EACH ROW EXECUTE FUNCTION agent_definitions_closed();
