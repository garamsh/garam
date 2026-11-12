-- The policy feed names an entity type by its qualified name, the
-- namespace of the manifest that declares it (ADR-0143 item 1): `Org`
-- is `Iam::Org`. The outbox, the function that marks it and the one
-- that reads an entity's state are restated to say so.
--
-- What this does to stored rows: policy_outbox holds marks awaiting
-- delivery and nothing else, and each is rewritten in place from `T`
-- to `Iam::T`, so a mark written before this migration is delivered
-- under the name the service now accepts. A mark of a type the CHECK
-- below does not list cannot exist, so none is dropped. No other table
-- is touched. The CHECK keeps the name it had.

ALTER TABLE policy_outbox DROP CONSTRAINT policy_outbox_entity_type_check;

UPDATE policy_outbox SET entity_type = 'Iam::' || entity_type;

ALTER TABLE policy_outbox ADD CONSTRAINT policy_outbox_entity_type_check
    CHECK (entity_type IN
        ('Iam::Org', 'Iam::OrgMembers', 'Iam::Team', 'Iam::Project',
         'Iam::User', 'Iam::Agent', 'Iam::Operator'));

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
        WHEN 'org_members' THEN
            PERFORM policy_outbox_mark('Iam::User', v_row->>'user_grn');
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

-- The shapes are the policy service's schema (iam.cedarschema), in the
-- qualified names the manifest writes.
CREATE OR REPLACE FUNCTION policy_entity_state(p_type text, p_id text)
RETURNS TABLE (org text, members text, parent_types text[], parent_ids text[])
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
    ), parents (ptype, pid) AS (
        SELECT 'Iam::Org', s.org FROM self s
            WHERE p_type IN ('Iam::Team', 'Iam::Project', 'Iam::Agent', 'Iam::Operator')
        UNION ALL
        SELECT 'Iam::OrgMembers', s.org FROM self s WHERE p_type = 'Iam::Agent'
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
                    FILTER (WHERE p.ptype IS NOT NULL), '{}'::text[])
    FROM self s LEFT JOIN parents p ON true
    GROUP BY s.org, s.members
$$;
