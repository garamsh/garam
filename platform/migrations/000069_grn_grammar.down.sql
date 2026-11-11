-- The CHECKs 000069 restated are dropped and the five-segment CHECKs
-- of 000068 are added back NOT VALID: a rollback of an earlier
-- migration drops them by name, so they must exist. NOT VALID leaves
-- the rows already stored unjudged by a CHECK that reads the
-- five-segment positions; a row written afterwards is judged by it.
-- The trigger function and the outbox function are restored as the
-- five-segment form read them.
ALTER TABLE agents DROP CONSTRAINT agents_grn_ownership;
ALTER TABLE agent_assignments
    DROP CONSTRAINT agent_assignments_assignee_service_type,
    DROP CONSTRAINT agent_assignments_same_org;
ALTER TABLE agent_watches DROP CONSTRAINT agent_watches_agent_service_type;
ALTER TABLE operators
    DROP CONSTRAINT operators_grn_ownership,
    DROP CONSTRAINT operators_manager;
ALTER TABLE operator_delegations DROP CONSTRAINT operator_delegations_ownership;
ALTER TABLE agent_enrollments DROP CONSTRAINT agent_enrollments_ownership;
ALTER TABLE agent_supervisors DROP CONSTRAINT agent_supervisors_ownership;
ALTER TABLE operator_tokens DROP CONSTRAINT operator_tokens_audience_service_type;
ALTER TABLE agent_tokens DROP CONSTRAINT agent_tokens_audience_service_type;
ALTER TABLE machine_authentication_evidence
    DROP CONSTRAINT machine_authentication_evidence_ownership;
ALTER TABLE audit_credential_events DROP CONSTRAINT audit_credential_events_ownership;

ALTER TABLE agent_assignments
    ADD CONSTRAINT agent_assignments_assignee_service_type
    CHECK ((split_part(assignee_grn, ':'::text, 4) = ANY (ARRAY['operator'::text, 'supervisor'::text]))) NOT VALID;
ALTER TABLE agent_assignments
    ADD CONSTRAINT agent_assignments_same_org
    CHECK ((split_part(assignee_grn, ':'::text, 2) = split_part(agent_grn, ':'::text, 2))) NOT VALID;
ALTER TABLE agent_enrollments
    ADD CONSTRAINT agent_enrollments_ownership
    CHECK (((split_part(agent_grn, ':'::text, 4) = 'agent'::text) AND (split_part(supervisor_grn, ':'::text, 4) = 'supervisor'::text) AND (split_part(agent_grn, ':'::text, 2) = split_part(org_grn, ':'::text, 5)) AND (split_part(supervisor_grn, ':'::text, 2) = split_part(org_grn, ':'::text, 5)))) NOT VALID;
ALTER TABLE agents
    ADD CONSTRAINT agents_grn_ownership
    CHECK (((split_part(grn, ':'::text, 4) = 'agent'::text) AND (split_part(grn, ':'::text, 2) = split_part(org_grn, ':'::text, 5)))) NOT VALID;
ALTER TABLE agent_supervisors
    ADD CONSTRAINT agent_supervisors_ownership
    CHECK (((split_part(grn, ':'::text, 4) = 'supervisor'::text) AND (split_part(grn, ':'::text, 2) = split_part(agent_grn, ':'::text, 2)))) NOT VALID;
ALTER TABLE agent_tokens
    ADD CONSTRAINT agent_tokens_audience_service_type
    CHECK (((audience_grn IS NULL) OR (split_part(audience_grn, ':'::text, 4) = 'operator'::text))) NOT VALID;
ALTER TABLE agent_watches
    ADD CONSTRAINT agent_watches_agent_service_type
    CHECK ((split_part(agent_grn, ':'::text, 4) = 'agent'::text)) NOT VALID;
ALTER TABLE audit_credential_events
    ADD CONSTRAINT audit_credential_events_ownership
    CHECK ((split_part(subject_grn, ':'::text, 2) = split_part(org_grn, ':'::text, 5))) NOT VALID;
ALTER TABLE machine_authentication_evidence
    ADD CONSTRAINT machine_authentication_evidence_ownership
    CHECK ((split_part(caller_grn, ':'::text, 2) = split_part(org_grn, ':'::text, 5))) NOT VALID;
ALTER TABLE operator_delegations
    ADD CONSTRAINT operator_delegations_ownership
    CHECK (((split_part(operator_grn, ':'::text, 4) = 'operator'::text) AND (split_part(operator_grn, ':'::text, 2) = split_part(org_grn, ':'::text, 5)))) NOT VALID;
ALTER TABLE operators
    ADD CONSTRAINT operators_grn_ownership
    CHECK (((split_part(grn, ':'::text, 4) = 'operator'::text) AND (split_part(grn, ':'::text, 2) = split_part(org_grn, ':'::text, 5)))) NOT VALID;
ALTER TABLE operators
    ADD CONSTRAINT operators_manager
    CHECK (((manager_grn IS NULL) OR ((manager_grn <> grn) AND (split_part(manager_grn, ':'::text, 4) = 'operator'::text) AND (split_part(manager_grn, ':'::text, 2) = split_part(grn, ':'::text, 2))))) NOT VALID;
ALTER TABLE operator_tokens
    ADD CONSTRAINT operator_tokens_audience_service_type
    CHECK (((audience_grn IS NULL) OR (split_part(audience_grn, ':'::text, 4) = 'operator'::text))) NOT VALID;

CREATE OR REPLACE FUNCTION policy_attachments_same_org() RETURNS trigger AS $$
DECLARE
    subject_org text := split_part(NEW.subject_grn, ':', 2);
    policy_org  text;
BEGIN
    SELECT split_part(org_grn, ':', 5) INTO policy_org FROM policies WHERE grn = NEW.policy_grn;
    IF policy_org IS NOT NULL AND subject_org <> 'root' AND subject_org <> policy_org THEN
        RAISE EXCEPTION 'policy attachment crosses orgs';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

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
            CASE split_part(v_member, ':', 4)
                WHEN 'user' THEN PERFORM policy_outbox_mark('User', v_member);
                WHEN 'agent' THEN PERFORM policy_outbox_mark('Agent', v_member);
                ELSE NULL;
            END CASE;
    END CASE;
    RETURN NULL;
END
$$;

DROP FUNCTION grn_id(text);
DROP FUNCTION grn_type(text);
DROP FUNCTION grn_service(text);
DROP FUNCTION grn_org(text);
DROP FUNCTION grn_segments(text);
