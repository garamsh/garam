-- The unqualified names, as 000067 and 000069 wrote them. A mark of a
-- type outside `Iam::` cannot exist under the CHECK being dropped, so
-- stripping the prefix loses none. The CHECK is restored under the name
-- it had.
ALTER TABLE policy_outbox DROP CONSTRAINT policy_outbox_entity_type_check;

UPDATE policy_outbox SET entity_type = substr(entity_type, length('Iam::') + 1);

ALTER TABLE policy_outbox ADD CONSTRAINT policy_outbox_entity_type_check
    CHECK (entity_type IN
        ('Org', 'OrgMembers', 'Team', 'Project', 'User', 'Agent', 'Operator'));

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

CREATE OR REPLACE FUNCTION policy_entity_state(p_type text, p_id text)
RETURNS TABLE (org text, members text, parent_types text[], parent_ids text[])
LANGUAGE sql STABLE AS $$
    WITH self AS (
        SELECT grn AS org, grn AS members FROM orgs
            WHERE p_type = 'Org' AND grn = p_id
        UNION ALL
        SELECT NULL, NULL FROM orgs
            WHERE p_type = 'OrgMembers' AND grn = p_id
        UNION ALL
        SELECT org_grn, NULL FROM groups
            WHERE p_type = 'Team' AND grn = p_id
        UNION ALL
        SELECT org_grn, NULL FROM projects
            WHERE p_type = 'Project' AND grn = p_id
        UNION ALL
        SELECT NULL, NULL FROM users
            WHERE p_type = 'User' AND grn = p_id
        UNION ALL
        SELECT org_grn, NULL FROM agents
            WHERE p_type = 'Agent' AND grn = p_id
        UNION ALL
        SELECT org_grn, NULL FROM operators
            WHERE p_type = 'Operator' AND grn = p_id
    ), parents (ptype, pid) AS (
        SELECT 'Org', s.org FROM self s
            WHERE p_type IN ('Team', 'Project', 'Agent', 'Operator')
        UNION ALL
        SELECT 'OrgMembers', s.org FROM self s WHERE p_type = 'Agent'
        UNION ALL
        SELECT 'OrgMembers', m.org_grn FROM org_members m
            WHERE p_type = 'User' AND m.user_grn = p_id
        UNION ALL
        SELECT 'Team', m.group_grn FROM group_members m
            WHERE p_type IN ('User', 'Agent') AND m.member_grn = p_id
        UNION ALL
        SELECT 'Project', m.project_grn FROM project_members m
            WHERE p_type IN ('User', 'Agent') AND m.member_grn = p_id
    )
    SELECT s.org, s.members,
           COALESCE(array_agg(p.ptype ORDER BY p.ptype, p.pid)
                    FILTER (WHERE p.ptype IS NOT NULL), '{}'::text[]),
           COALESCE(array_agg(p.pid ORDER BY p.ptype, p.pid)
                    FILTER (WHERE p.ptype IS NOT NULL), '{}'::text[])
    FROM self s LEFT JOIN parents p ON true
    GROUP BY s.org, s.members
$$;
