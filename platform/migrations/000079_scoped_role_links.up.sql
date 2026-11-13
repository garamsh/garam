-- A group's and a project's roles reach the policy service as scoped
-- role links the platform writes (ADR-0162), and a user's direct roles
-- are removed. A third kind of mark is added, `Iam::ScopedRoleLink`,
-- whose id is the GRN of the group or the project: the one entity the
-- link has as both principal and resource (ADR-0163). A write of the
-- roles, and the end of the group or the project, mark it.
--
-- What this does to stored rows: users.roles is dropped with what it
-- held, which no reader of the platform reads any more. groups and
-- projects are read and not written. policy_outbox keeps every mark. A
-- feed that is already on marks each group and each project, below, so
-- the roles stored before this migration are delivered.

-- groups and projects are locked before policy_outbox, the order a
-- writer of either takes (its row, then the mark its trigger writes)
-- and `garam policy-feed start` takes (postgres.go lockSources), so a
-- live writer and this migration wait on each other and never
-- deadlock.
LOCK TABLE groups, projects IN SHARE MODE;

ALTER TABLE users DROP COLUMN roles;

ALTER TABLE policy_outbox DROP CONSTRAINT policy_outbox_entity_type_check;

ALTER TABLE policy_outbox ADD CONSTRAINT policy_outbox_entity_type_check
    CHECK (entity_type IN
        ('Iam::Org', 'Iam::OrgMembers', 'Iam::Team', 'Iam::Project',
         'Iam::User', 'Iam::Agent', 'Iam::Operator', 'Iam::RoleLink',
         'Iam::ScopedRoleLink'));

-- A scoped link, like a member's, may be set aside.
ALTER TABLE policy_outbox DROP CONSTRAINT policy_outbox_set_aside_check;

ALTER TABLE policy_outbox ADD CONSTRAINT policy_outbox_set_aside_check CHECK (
    (set_aside_code IS NULL) = (set_aside_at IS NULL)
    AND (set_aside_reason IS NULL) = (set_aside_at IS NULL)
    AND (set_aside_at IS NULL
         OR entity_type IN ('Iam::RoleLink', 'Iam::ScopedRoleLink')));

-- A group or a project marks its entity when it starts and when it
-- ends, as before, and its scoped link on those and when its roles
-- change. A write of the name marks nothing.
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

CREATE TRIGGER policy_outbox_groups_roles AFTER UPDATE OF roles ON groups
    FOR EACH ROW EXECUTE FUNCTION policy_outbox_track();

CREATE TRIGGER policy_outbox_projects_roles AFTER UPDATE OF roles ON projects
    FOR EACH ROW EXECUTE FUNCTION policy_outbox_track();

-- policy_scoped_link_state returns the roles the group or the project
-- named by p_id holds, each once, and a role the platform does not
-- know is left out, as a role that grants nothing is. No row is a
-- group or a project the table no longer holds, which holds no link.
CREATE FUNCTION policy_scoped_link_state(p_id text) RETURNS TABLE (roles text[])
LANGUAGE sql STABLE AS $$
    SELECT ARRAY(SELECT DISTINCT r FROM unnest(g.roles) AS r
                 WHERE r IN ('owner', 'admin', 'member') ORDER BY r)
    FROM groups g WHERE g.grn = p_id
    UNION ALL
    SELECT ARRAY(SELECT DISTINCT r FROM unnest(p.roles) AS r
                 WHERE r IN ('owner', 'admin', 'member') ORDER BY r)
    FROM projects p WHERE p.grn = p_id
$$;

-- A feed that is already on was started before roles were marked, and
-- holds no link for the roles that exist. Marking every group and
-- project is what `garam policy-feed start` does for a feed that is
-- off, and a mark already there only has its generation raised.
INSERT INTO policy_outbox (entity_type, entity_id)
SELECT 'Iam::ScopedRoleLink', grn FROM groups WHERE (SELECT enabled FROM policy_feed)
UNION ALL
SELECT 'Iam::ScopedRoleLink', grn FROM projects WHERE (SELECT enabled FROM policy_feed)
ON CONFLICT (entity_type, entity_id)
DO UPDATE SET generation = policy_outbox.generation + 1;
