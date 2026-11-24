-- An installation and an instance reach the policy service as an
-- `Iam::Installation` and an `Iam::Instance` (ADR-0192 decisions 2, 3
-- and 9): an installation in its organization's `Iam::OrgMembers`
-- beside its `Iam::Org`, and an instance in its installation, so an
-- instance's principal holds what its installation is granted and a
-- decision on an installation reaches the instances in it. Each id is
-- the record's GRN.
--
-- An installation marks itself when it is written or deleted, and an
-- instance likewise; an installation's deletion deletes its instances,
-- and each of those deletions marks the instance. Neither row's other
-- columns reach the entity, so an update of them marks nothing.
--
-- What this does to stored rows: installations and instances are read
-- and not written, and policy_outbox keeps every mark. A feed that is
-- already on marks each current row, below, so a record made before
-- this migration is delivered.

-- installations and instances are locked before policy_outbox, the
-- order a writer of either takes (its row, then the mark its trigger
-- writes) and `garam policy-feed start` takes (postgres.go
-- lockSources).
LOCK TABLE installations, instances IN SHARE MODE;

ALTER TABLE policy_outbox DROP CONSTRAINT policy_outbox_entity_type_check;

ALTER TABLE policy_outbox ADD CONSTRAINT policy_outbox_entity_type_check
    CHECK (entity_type IN
        ('Iam::Org', 'Iam::OrgMembers', 'Iam::Team', 'Iam::Project',
         'Iam::User', 'Iam::Agent', 'Iam::Operator', 'Iam::RoleLink',
         'Iam::ScopedRoleLink', 'Iam::Supervisor', 'Iam::Installation',
         'Iam::Instance'));

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
        WHEN 'installations' THEN
            PERFORM policy_outbox_mark('Iam::Installation', v_row->>'grn');
        WHEN 'instances' THEN
            PERFORM policy_outbox_mark('Iam::Instance', v_row->>'grn');
        WHEN 'agent_supervisors' THEN
            PERFORM policy_outbox_mark('Iam::Supervisor', v_row->>'grn');
        WHEN 'agent_assignments' THEN
            -- An agent moving from one assignee to another changes
            -- what each supervisor holds, so both are marked.
            IF grn_type(v_old->>'assignee_grn') = 'supervisor' THEN
                PERFORM policy_outbox_mark('Iam::Supervisor', v_old->>'assignee_grn');
            END IF;
            IF grn_type(v_row->>'assignee_grn') = 'supervisor' THEN
                PERFORM policy_outbox_mark('Iam::Supervisor', v_row->>'assignee_grn');
            END IF;
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

CREATE TRIGGER policy_outbox_installations
    AFTER INSERT OR DELETE
    ON installations
    FOR EACH ROW EXECUTE FUNCTION policy_outbox_track();

CREATE TRIGGER policy_outbox_instances
    AFTER INSERT OR DELETE
    ON instances
    FOR EACH ROW EXECUTE FUNCTION policy_outbox_track();

-- An installation's parents are its organization and the
-- organization's members, and an instance's is its installation. The
-- return type is 000080's, so the function is replaced in place.
CREATE OR REPLACE FUNCTION policy_entity_state(p_type text, p_id text)
RETURNS TABLE (org text, members text, parent_types text[], parent_ids text[],
               consent_ids text[], operations text[], agent text)
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
        UNION ALL
        SELECT org_grn, NULL FROM installations
            WHERE p_type = 'Iam::Installation' AND grn = p_id
        UNION ALL
        SELECT org_grn, NULL FROM instances
            WHERE p_type = 'Iam::Instance' AND grn = p_id        UNION ALL
        SELECT g.org_grn, NULL FROM agent_supervisors v
            JOIN agents g ON g.grn = v.agent_grn
            WHERE p_type = 'Iam::Supervisor' AND v.grn = p_id
              AND v.ended_at IS NULL
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
    ), holding (agent) AS (
        SELECT v.agent_grn FROM agent_supervisors v
            JOIN agent_assignments a
              ON a.agent_grn = v.agent_grn AND a.assignee_grn = v.grn
        WHERE p_type = 'Iam::Supervisor' AND v.grn = p_id
          AND v.ended_at IS NULL
    ), parents (ptype, pid) AS (
        SELECT 'Iam::Org', s.org FROM self s
            WHERE p_type IN ('Iam::Team', 'Iam::Project', 'Iam::Agent', 'Iam::Operator',
                             'Iam::Supervisor', 'Iam::Installation')
        UNION ALL
        SELECT 'Iam::OrgMembers', s.org FROM self s
            WHERE p_type IN ('Iam::Agent', 'Iam::Operator', 'Iam::Supervisor',
                             'Iam::Installation')
        UNION ALL
        SELECT 'Iam::Installation', i.installation_grn FROM instances i
            WHERE p_type = 'Iam::Instance' AND i.grn = p_id
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
           ARRAY(SELECT h.op FROM held h ORDER BY h.op COLLATE "C"),
           (SELECT w.agent FROM holding w)
    FROM self s LEFT JOIN parents p ON true
    GROUP BY s.org, s.members
$$;

-- A feed that is already on holds no installation or instance.
-- Marking every one is what `garam policy-feed start` does for a feed
-- that is off, and a mark already there only has its generation
-- raised.
INSERT INTO policy_outbox (entity_type, entity_id)
SELECT t, id FROM (
    SELECT 'Iam::Installation' AS t, grn AS id FROM installations
    UNION ALL SELECT 'Iam::Instance', grn FROM instances
) live
WHERE (SELECT enabled FROM policy_feed)
ON CONFLICT (entity_type, entity_id)
DO UPDATE SET generation = policy_outbox.generation + 1;
