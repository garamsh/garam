-- The policy service's feed (ADR-0140): a set of dirty entities that
-- triggers write and a later delivery loop reads. It holds no document;
-- policy_entity_state reads what an entity is when it is delivered.

-- One row, off by default, so a deployment with no policy service
-- writes no mark and takes no lock on the outbox (ADR-0140 §8).
CREATE TABLE policy_feed (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    enabled   boolean NOT NULL DEFAULT false
);

INSERT INTO policy_feed (singleton) VALUES (true);

-- One row per entity, so the table is bounded by the entities written
-- since the feed started and needs no sweeper (ADR-0140 §9). A write
-- raises the generation: delivery deletes a mark only at the
-- generation it read, so a write landing mid-pass survives it.
CREATE TABLE policy_outbox (
    entity_type text   NOT NULL CHECK (entity_type IN
        ('Org', 'OrgMembers', 'Team', 'Project', 'User', 'Agent', 'Operator')),
    entity_id   text   NOT NULL,
    generation  bigint NOT NULL DEFAULT 1 CHECK (generation > 0),
    PRIMARY KEY (entity_type, entity_id)
);

CREATE FUNCTION policy_outbox_mark(p_type text, p_id text) RETURNS void
LANGUAGE sql VOLATILE AS $$
    INSERT INTO policy_outbox (entity_type, entity_id) VALUES (p_type, p_id)
    ON CONFLICT (entity_type, entity_id)
    DO UPDATE SET generation = policy_outbox.generation + 1
$$;

-- The trigger of every source table below. It fires on INSERT and
-- DELETE only: no store updates a key, a containment column or a
-- membership, and an update of a role or a name reaches no entity.
-- A cascade fires it row by row, so a deleted organization marks
-- what its foreign keys took with it.
--
-- A member is a user or an agent, told by its GRN's service type, and
-- group_members.member_grn and project_members.member_grn carry no
-- foreign key, so a GRN of any other type marks nothing.
CREATE FUNCTION policy_outbox_track() RETURNS trigger
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

CREATE TRIGGER policy_outbox_orgs AFTER INSERT OR DELETE ON orgs
    FOR EACH ROW EXECUTE FUNCTION policy_outbox_track();
CREATE TRIGGER policy_outbox_groups AFTER INSERT OR DELETE ON groups
    FOR EACH ROW EXECUTE FUNCTION policy_outbox_track();
CREATE TRIGGER policy_outbox_projects AFTER INSERT OR DELETE ON projects
    FOR EACH ROW EXECUTE FUNCTION policy_outbox_track();
CREATE TRIGGER policy_outbox_org_members AFTER INSERT OR DELETE ON org_members
    FOR EACH ROW EXECUTE FUNCTION policy_outbox_track();
CREATE TRIGGER policy_outbox_group_members AFTER INSERT OR DELETE ON group_members
    FOR EACH ROW EXECUTE FUNCTION policy_outbox_track();
CREATE TRIGGER policy_outbox_project_members AFTER INSERT OR DELETE ON project_members
    FOR EACH ROW EXECUTE FUNCTION policy_outbox_track();
CREATE TRIGGER policy_outbox_agents AFTER INSERT OR DELETE ON agents
    FOR EACH ROW EXECUTE FUNCTION policy_outbox_track();
CREATE TRIGGER policy_outbox_operators AFTER INSERT OR DELETE ON operators
    FOR EACH ROW EXECUTE FUNCTION policy_outbox_track();
CREATE TRIGGER policy_outbox_users AFTER INSERT OR DELETE ON users
    FOR EACH ROW EXECUTE FUNCTION policy_outbox_track();

-- policy_entity_state is the one surface a delivery reads an entity's
-- state through, as agent_execution_admit is the one an execution is
-- (ADR-0080, ADR-0140 §6). It answers no row for an entity the owners'
-- tables no longer hold, which is a delete, and otherwise one row: the
-- entity's `org` and `members` references, which name an Org and an
-- OrgMembers by the organization's GRN, and its parents as two
-- arrays of one length, in type then id order.
--
-- The shapes are the policy service's schema (garam.cedarschema): an
-- Org names itself; a Team, a Project and an Operator sit in their
-- Org; a User sits in the OrgMembers of each organization it belongs
-- to and in each Team and Project it is a member of; an Agent is
-- both a member and a contained resource, so it sits in its Org and
-- its OrgMembers as well as in its Teams and Projects (ADR-0137).
--
-- STABLE, so every row it reads comes from one snapshot.
CREATE FUNCTION policy_entity_state(p_type text, p_id text)
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
