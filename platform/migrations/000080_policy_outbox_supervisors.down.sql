-- The outbox as 000079 left it. The supervisors the service holds
-- stay there, with the agent each held, until an operator removes
-- them: no pass of the older schema reads their kind. Marks of the
-- kind are deleted, as no state function of the older schema reads
-- them.
DROP TRIGGER policy_outbox_agent_supervisors ON agent_supervisors;
DROP TRIGGER policy_outbox_agent_assignments ON agent_assignments;

DELETE FROM policy_outbox WHERE entity_type = 'Iam::Supervisor';

ALTER TABLE policy_outbox DROP CONSTRAINT policy_outbox_entity_type_check;

ALTER TABLE policy_outbox ADD CONSTRAINT policy_outbox_entity_type_check
    CHECK (entity_type IN
        ('Iam::Org', 'Iam::OrgMembers', 'Iam::Team', 'Iam::Project',
         'Iam::User', 'Iam::Agent', 'Iam::Operator', 'Iam::RoleLink',
         'Iam::ScopedRoleLink'));

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
            IF TG_OP <> 'UPDATE' THEN
                PERFORM policy_outbox_mark('Iam::Team', v_row->>'grn');
            END IF;
            IF TG_OP <> 'UPDATE'
               OR (v_old->'roles') IS DISTINCT FROM (v_row->'roles') THEN
                PERFORM policy_outbox_mark('Iam::ScopedRoleLink', v_row->>'grn');
            END IF;
        WHEN 'projects' THEN
            IF TG_OP <> 'UPDATE' THEN
                PERFORM policy_outbox_mark('Iam::Project', v_row->>'grn');
            END IF;
            IF TG_OP <> 'UPDATE'
               OR (v_old->'roles') IS DISTINCT FROM (v_row->'roles') THEN
                PERFORM policy_outbox_mark('Iam::ScopedRoleLink', v_row->>'grn');
            END IF;
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

DROP FUNCTION policy_entity_state(text, text);

CREATE FUNCTION policy_entity_state(p_type text, p_id text)
RETURNS TABLE (org text, members text, parent_types text[], parent_ids text[],
               consent_ids text[], operations text[])
LANGUAGE sql STABLE AS $$
    WITH self AS (
        SELECT grn AS org, grn AS members FROM orgs
            WHERE p_type = 'Iam::Org' AND grn = p_id
        UNION ALL
        SELECT NULL, NULL FROM orgs
            WHERE p_type = 'Iam::OrgMembers' AND grn = p_id
        UNION ALL
        SELECT org_grn, NULL FROM groups
            WHERE p_type = 'Iam::Team' AND grn = p_id
        UNION ALL
        SELECT org_grn, NULL FROM projects
            WHERE p_type = 'Iam::Project' AND grn = p_id
        UNION ALL
        SELECT NULL, NULL FROM users
            WHERE p_type = 'Iam::User' AND grn = p_id
        UNION ALL
        SELECT org_grn, NULL FROM agents
            WHERE p_type = 'Iam::Agent' AND grn = p_id
        UNION ALL
        SELECT org_grn, NULL FROM operators
            WHERE p_type = 'Iam::Operator' AND grn = p_id
    ), consents (other) AS (
        SELECT CASE WHEN m.org_a = p_id THEN m.org_b ELSE m.org_a END
        FROM message_consents m
        WHERE p_type = 'Iam::Org'
          AND ((m.org_a = p_id AND m.a_consented_at IS NOT NULL)
            OR (m.org_b = p_id AND m.b_consented_at IS NOT NULL))
    ), held (op) AS (
        SELECT o FROM operator_delegations d, unnest(d.operations) AS o
        WHERE p_type = 'Iam::Operator' AND d.operator_grn = p_id
          AND d.ended_at IS NULL
    ), parents (ptype, pid) AS (
        SELECT 'Iam::Org', s.org FROM self s
            WHERE p_type IN ('Iam::Team', 'Iam::Project', 'Iam::Agent', 'Iam::Operator')
        UNION ALL
        SELECT 'Iam::OrgMembers', s.org FROM self s
            WHERE p_type IN ('Iam::Agent', 'Iam::Operator')
        UNION ALL
        SELECT 'Iam::OrgMembers', m.org_grn FROM org_members m
            WHERE p_type = 'Iam::User' AND m.user_grn = p_id
        UNION ALL
        SELECT 'Iam::Team', m.group_grn FROM group_members m
            WHERE p_type IN ('Iam::User', 'Iam::Agent') AND m.member_grn = p_id
        UNION ALL
        SELECT 'Iam::Project', m.project_grn FROM project_members m
            WHERE p_type IN ('Iam::User', 'Iam::Agent') AND m.member_grn = p_id
    )
    SELECT s.org, s.members,
           COALESCE(array_agg(p.ptype ORDER BY p.ptype, p.pid)
                    FILTER (WHERE p.ptype IS NOT NULL), '{}'::text[]),
           COALESCE(array_agg(p.pid ORDER BY p.ptype, p.pid)
                    FILTER (WHERE p.ptype IS NOT NULL), '{}'::text[]),
           CASE WHEN p_type = 'Iam::Org' THEN
               ARRAY(SELECT c.other FROM consents c ORDER BY c.other)
           ELSE '{}'::text[] END,
           ARRAY(SELECT h.op FROM held h ORDER BY h.op COLLATE "C")
    FROM self s LEFT JOIN parents p ON true
    GROUP BY s.org, s.members
$$;
