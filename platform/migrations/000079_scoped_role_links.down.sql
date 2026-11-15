-- The schema as 000078 left it. users.roles comes back empty: what it
-- held is not kept. The scoped links the service holds for the roles
-- of groups and projects stay there, as no pass removes them, until
-- the migration is applied again, which marks every group and every
-- project of a feed that is on. Their marks are deleted, as no state
-- function of the older schema reads them.
DROP TRIGGER policy_outbox_groups_roles ON groups;
DROP TRIGGER policy_outbox_projects_roles ON projects;

DROP FUNCTION policy_scoped_link_state(text);

DELETE FROM policy_outbox WHERE entity_type = 'Iam::ScopedRoleLink';

ALTER TABLE policy_outbox DROP CONSTRAINT policy_outbox_set_aside_check;

ALTER TABLE policy_outbox ADD CONSTRAINT policy_outbox_set_aside_check CHECK (
    (set_aside_code IS NULL) = (set_aside_at IS NULL)
    AND (set_aside_reason IS NULL) = (set_aside_at IS NULL)
    AND (set_aside_at IS NULL OR entity_type = 'Iam::RoleLink'));

ALTER TABLE policy_outbox DROP CONSTRAINT policy_outbox_entity_type_check;

ALTER TABLE policy_outbox ADD CONSTRAINT policy_outbox_entity_type_check
    CHECK (entity_type IN
        ('Iam::Org', 'Iam::OrgMembers', 'Iam::Team', 'Iam::Project',
         'Iam::User', 'Iam::Agent', 'Iam::Operator', 'Iam::RoleLink'));

ALTER TABLE users ADD COLUMN roles text[] NOT NULL DEFAULT '{}';

CREATE OR REPLACE FUNCTION policy_outbox_track() RETURNS trigger
LANGUAGE plpgsql VOLATILE AS $$
DECLARE
    v_row jsonb;
    v_member text;
    v_old jsonb;
BEGIN
    IF NOT (SELECT enabled FROM policy_feed) THEN
        RETURN NULL;
    END IF;
    v_row := CASE TG_OP WHEN 'DELETE' THEN to_jsonb(OLD) ELSE to_jsonb(NEW) END;
    v_old := CASE TG_OP WHEN 'INSERT' THEN NULL ELSE to_jsonb(OLD) END;
    CASE TG_TABLE_NAME
        WHEN 'orgs' THEN
            PERFORM policy_outbox_mark('Iam::Org', v_row->>'grn');
            PERFORM policy_outbox_mark('Iam::OrgMembers', v_row->>'grn');
        WHEN 'groups' THEN
            PERFORM policy_outbox_mark('Iam::Team', v_row->>'grn');
        WHEN 'projects' THEN
            PERFORM policy_outbox_mark('Iam::Project', v_row->>'grn');
        WHEN 'users' THEN
            PERFORM policy_outbox_mark('Iam::User', v_row->>'grn');
        WHEN 'agents' THEN
            PERFORM policy_outbox_mark('Iam::Agent', v_row->>'grn');
        WHEN 'operators' THEN
            PERFORM policy_outbox_mark('Iam::Operator', v_row->>'grn');
        WHEN 'operator_delegations' THEN
            PERFORM policy_outbox_mark('Iam::Operator', v_row->>'operator_grn');
        WHEN 'org_members' THEN
            PERFORM policy_outbox_mark('Iam::RoleLink',
                (v_row->>'org_grn') || ' ' || (v_row->>'user_grn'));
            IF TG_OP <> 'UPDATE' THEN
                PERFORM policy_outbox_mark('Iam::User', v_row->>'user_grn');
            END IF;
        WHEN 'message_consents' THEN
            IF ((v_old->>'a_consented_at') IS NOT NULL)
               <> (TG_OP <> 'DELETE' AND (v_row->>'a_consented_at') IS NOT NULL) THEN
                PERFORM policy_outbox_mark('Iam::Org', v_row->>'org_a');
            END IF;
            IF ((v_old->>'b_consented_at') IS NOT NULL)
               <> (TG_OP <> 'DELETE' AND (v_row->>'b_consented_at') IS NOT NULL) THEN
                PERFORM policy_outbox_mark('Iam::Org', v_row->>'org_b');
            END IF;
        WHEN 'group_members', 'project_members' THEN
            v_member := v_row->>'member_grn';
            IF grn_service(v_member) = 'iam' THEN
                CASE grn_type(v_member)
                    WHEN 'user' THEN PERFORM policy_outbox_mark('Iam::User', v_member);
                    WHEN 'agent' THEN PERFORM policy_outbox_mark('Iam::Agent', v_member);
                    ELSE NULL;
                END CASE;
            END IF;
    END CASE;
    RETURN NULL;
END
$$;
