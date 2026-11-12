-- A GRN names its organization, its service and a path of type and id
-- pairs (ADR-0142). The CHECKs and the trigger that tied a name's type
-- and organization to the row holding it read the five-segment form at
-- fixed positions; they are restated here through four functions that
-- read a name in the new grammar, and lib/grn is the other reader that
-- the tests hold them to.
--
-- What this does to a database holding a five-segment name: it
-- rewrites no row. Each CHECK is added NOT VALID, so the migration
-- does not fail on such a row and does not validate it; a write that
-- inserts a row of that form is refused, and so is an update of a
-- stored one, which is rewritten by a means outside this migration.
-- The trigger acts on the rows written after it, as it always did.

-- The segments of a name before '#', or NULL where the text is not a
-- GRN of the grammar: no 'grn:' prefix, fewer than an organization, a
-- service and one type and id pair, or half a pair.
CREATE FUNCTION grn_segments(g text) RETURNS text[]
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT CASE
        WHEN g LIKE 'grn:%' AND cardinality(s) >= 4 AND cardinality(s) % 2 = 0 THEN s
    END
    FROM (SELECT string_to_array(split_part(substr(g, 5), '#', 1), '/') AS s) q
$$;

-- The four readers return the empty string for a text that is not a
-- GRN, so that a comparison against a type or a column does not pass
-- on NULL. A NULL argument gives NULL, as a nullable column needs.
CREATE FUNCTION grn_org(g text) RETURNS text
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT COALESCE((grn_segments(g))[1], '')
$$;

CREATE FUNCTION grn_service(g text) RETURNS text
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT COALESCE((grn_segments(g))[2], '')
$$;

CREATE FUNCTION grn_type(g text) RETURNS text
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT COALESCE((SELECT a[cardinality(a) - 1] FROM (SELECT grn_segments(g) AS a) q), '')
$$;

CREATE FUNCTION grn_id(g text) RETURNS text
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT COALESCE((SELECT a[cardinality(a)] FROM (SELECT grn_segments(g) AS a) q), '')
$$;

-- Every CHECK that read a name with split_part, dropped and restated.
-- An organization's own id is the last segment of its GRN, so a name
-- belongs to the organization when its first segment is that id.
ALTER TABLE agents
    DROP CONSTRAINT IF EXISTS agents_grn_ownership,
    ADD CONSTRAINT agents_grn_ownership
        CHECK (grn_service(grn) = 'iam' AND grn_type(grn) = 'agent'
           AND grn_org(grn) = grn_id(org_grn) AND grn_org(grn) <> '') NOT VALID;

ALTER TABLE agent_assignments
    DROP CONSTRAINT IF EXISTS agent_assignments_assignee_service_type,
    ADD CONSTRAINT agent_assignments_assignee_service_type
        CHECK (grn_service(assignee_grn) = 'iam'
           AND grn_type(assignee_grn) IN ('operator', 'supervisor')) NOT VALID,
    DROP CONSTRAINT IF EXISTS agent_assignments_same_org,
    ADD CONSTRAINT agent_assignments_same_org
        CHECK (grn_org(assignee_grn) = grn_org(agent_grn)
           AND grn_org(assignee_grn) <> '') NOT VALID;

ALTER TABLE agent_watches
    DROP CONSTRAINT IF EXISTS agent_watches_agent_service_type,
    ADD CONSTRAINT agent_watches_agent_service_type
        CHECK (grn_service(agent_grn) = 'iam'
           AND grn_type(agent_grn) = 'agent') NOT VALID;

ALTER TABLE operators
    DROP CONSTRAINT IF EXISTS operators_grn_ownership,
    ADD CONSTRAINT operators_grn_ownership
        CHECK (grn_service(grn) = 'iam' AND grn_type(grn) = 'operator'
           AND grn_org(grn) = grn_id(org_grn) AND grn_org(grn) <> '') NOT VALID,
    DROP CONSTRAINT IF EXISTS operators_manager,
    ADD CONSTRAINT operators_manager
        CHECK (manager_grn IS NULL
            OR (manager_grn <> grn
                AND grn_service(manager_grn) = 'iam'
                AND grn_type(manager_grn) = 'operator'
                AND grn_org(manager_grn) = grn_org(grn)
                AND grn_org(grn) <> '')) NOT VALID;

ALTER TABLE operator_delegations
    DROP CONSTRAINT IF EXISTS operator_delegations_ownership,
    ADD CONSTRAINT operator_delegations_ownership
        CHECK (grn_service(operator_grn) = 'iam'
           AND grn_type(operator_grn) = 'operator'
           AND grn_org(operator_grn) = grn_id(org_grn)
           AND grn_org(operator_grn) <> '') NOT VALID;

ALTER TABLE agent_enrollments
    DROP CONSTRAINT IF EXISTS agent_enrollments_ownership,
    ADD CONSTRAINT agent_enrollments_ownership
        CHECK (grn_service(agent_grn) = 'iam' AND grn_type(agent_grn) = 'agent'
           AND grn_service(supervisor_grn) = 'iam'
           AND grn_type(supervisor_grn) = 'supervisor'
           AND grn_org(agent_grn) = grn_id(org_grn)
           AND grn_org(supervisor_grn) = grn_id(org_grn)
           AND grn_org(agent_grn) <> '') NOT VALID;

ALTER TABLE agent_supervisors
    DROP CONSTRAINT IF EXISTS agent_supervisors_ownership,
    ADD CONSTRAINT agent_supervisors_ownership
        CHECK (grn_service(grn) = 'iam' AND grn_type(grn) = 'supervisor'
           AND grn_org(grn) = grn_org(agent_grn)
           AND grn_org(grn) <> '') NOT VALID;

ALTER TABLE operator_tokens
    DROP CONSTRAINT IF EXISTS operator_tokens_audience_service_type,
    ADD CONSTRAINT operator_tokens_audience_service_type
        CHECK (audience_grn IS NULL
            OR (grn_service(audience_grn) = 'iam'
                AND grn_type(audience_grn) = 'operator')) NOT VALID;

ALTER TABLE agent_tokens
    DROP CONSTRAINT IF EXISTS agent_tokens_audience_service_type,
    ADD CONSTRAINT agent_tokens_audience_service_type
        CHECK (audience_grn IS NULL
            OR (grn_service(audience_grn) = 'iam'
                AND grn_type(audience_grn) = 'operator')) NOT VALID;

ALTER TABLE machine_authentication_evidence
    DROP CONSTRAINT IF EXISTS machine_authentication_evidence_ownership,
    ADD CONSTRAINT machine_authentication_evidence_ownership
        CHECK (grn_org(caller_grn) = grn_id(org_grn)
           AND grn_org(caller_grn) <> '') NOT VALID;

ALTER TABLE audit_credential_events
    DROP CONSTRAINT IF EXISTS audit_credential_events_ownership,
    ADD CONSTRAINT audit_credential_events_ownership
        CHECK (grn_org(subject_grn) = grn_id(org_grn)
           AND grn_org(subject_grn) <> '') NOT VALID;

-- An attachment stays inside the policy's org (ADR-0008). The trigger
-- is the one it was, comparing the subject's organization with the
-- policy's; only the reading of the two names changed.
CREATE OR REPLACE FUNCTION policy_attachments_same_org() RETURNS trigger AS $$
DECLARE
    subject_org text := grn_org(NEW.subject_grn);
    policy_org  text;
BEGIN
    SELECT grn_id(org_grn) INTO policy_org FROM policies WHERE grn = NEW.policy_grn;
    IF policy_org IS NOT NULL AND subject_org <> 'root' AND subject_org <> policy_org THEN
        RAISE EXCEPTION 'policy attachment crosses orgs';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- A member of a group or a project is a user or an agent, told by its
-- name's service and type. A GRN of any other type marks nothing.
CREATE OR REPLACE FUNCTION policy_outbox_track() RETURNS trigger
LANGUAGE plpgsql VOLATILE AS $$
DECLARE
    v_row jsonb;
    v_member text;
BEGIN
    IF NOT (SELECT enabled FROM policy_feed) THEN
        RETURN NULL;
    END IF;
    v_row := CASE TG_OP WHEN 'DELETE' THEN to_jsonb(OLD) ELSE to_jsonb(NEW) END;
    CASE TG_TABLE_NAME
        WHEN 'orgs' THEN
            PERFORM policy_outbox_mark('Org', v_row->>'grn');
            PERFORM policy_outbox_mark('OrgMembers', v_row->>'grn');
        WHEN 'groups' THEN
            PERFORM policy_outbox_mark('Team', v_row->>'grn');
        WHEN 'projects' THEN
            PERFORM policy_outbox_mark('Project', v_row->>'grn');
        WHEN 'users' THEN
            PERFORM policy_outbox_mark('User', v_row->>'grn');
        WHEN 'agents' THEN
            PERFORM policy_outbox_mark('Agent', v_row->>'grn');
        WHEN 'operators' THEN
            PERFORM policy_outbox_mark('Operator', v_row->>'grn');
        WHEN 'org_members' THEN
            PERFORM policy_outbox_mark('User', v_row->>'user_grn');
        WHEN 'group_members', 'project_members' THEN
            v_member := v_row->>'member_grn';
            IF grn_service(v_member) = 'iam' THEN
                CASE grn_type(v_member)
                    WHEN 'user' THEN PERFORM policy_outbox_mark('User', v_member);
                    WHEN 'agent' THEN PERFORM policy_outbox_mark('Agent', v_member);
                    ELSE NULL;
                END CASE;
            END IF;
    END CASE;
    RETURN NULL;
END
$$;
