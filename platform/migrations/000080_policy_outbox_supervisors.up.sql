-- A supervisor reaches the policy service as an `Iam::Supervisor`
-- (ADR-0164): one per registration of a host's supervisor that stands,
-- in its organization's `Iam::OrgMembers` beside its `Iam::Org`, with
-- the agent it holds as `agent`. A fourth kind of entity is marked,
-- `Iam::Supervisor`, whose id is the supervisor's GRN.
--
-- A supervisor holds the agent its registration names when the agent's
-- assignment names that supervisor, which is what the platform's own
-- decision reads (`supervising`, platform/internal/cli/execution.go);
-- one that holds none is fed with no agent. A registration that has
-- ended has no state, so it is deleted.
--
-- What this does to stored rows: agent_supervisors and
-- agent_assignments are read and not written, and policy_outbox keeps
-- every mark. A feed that is already on marks each current
-- registration, below, so a registration made before this migration is
-- delivered, and one that ended while the feed was off is deleted.

-- agent_supervisors and agent_assignments are locked before
-- policy_outbox, the order a writer of either takes (its row, then
-- the mark its trigger writes) and `garam policy-feed start` takes
-- (postgres.go lockSources), so a live writer of either takes the same
-- locks in the same order as this migration.
LOCK TABLE agent_supervisors, agent_assignments IN SHARE MODE;

ALTER TABLE policy_outbox DROP CONSTRAINT policy_outbox_entity_type_check;

ALTER TABLE policy_outbox ADD CONSTRAINT policy_outbox_entity_type_check
    CHECK (entity_type IN
        ('Iam::Org', 'Iam::OrgMembers', 'Iam::Team', 'Iam::Project',
         'Iam::User', 'Iam::Agent', 'Iam::Operator', 'Iam::RoleLink',
         'Iam::ScopedRoleLink', 'Iam::Supervisor'));

-- A registration marks its supervisor when it starts, when it ends
-- and when its agent changes, and the cascade that deletes it with an
-- agent or an organization does too. An assignment marks the
-- supervisor it names, and the one it named before, when it is
-- written, moved or deleted. A renewal of the certificate writes
-- neither column and marks nothing.

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

CREATE TRIGGER policy_outbox_agent_supervisors
    AFTER INSERT OR DELETE OR UPDATE OF ended_at, agent_grn
    ON agent_supervisors
    FOR EACH ROW EXECUTE FUNCTION policy_outbox_track();

CREATE TRIGGER policy_outbox_agent_assignments
    AFTER INSERT OR DELETE OR UPDATE OF assignee_grn
    ON agent_assignments
    FOR EACH ROW EXECUTE FUNCTION policy_outbox_track();

-- The agent a supervisor holds is `agent`. The return type gains a
-- column, so the function is dropped and made again.
DROP FUNCTION policy_entity_state(text, text);

CREATE FUNCTION policy_entity_state(p_type text, p_id text)
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
                             'Iam::Supervisor')
        UNION ALL
        SELECT 'Iam::OrgMembers', s.org FROM self s
            WHERE p_type IN ('Iam::Agent', 'Iam::Operator', 'Iam::Supervisor')
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

-- A feed that is already on was started before registrations were
-- marked, and holds no supervisor. Marking every registration, ended
-- or not (an ended one has no state and is delivered as a delete), is
-- what `garam policy-feed start` does for a feed that is off, and a
-- mark already there only has its generation raised.
INSERT INTO policy_outbox (entity_type, entity_id)
SELECT 'Iam::Supervisor', grn FROM agent_supervisors
WHERE (SELECT enabled FROM policy_feed)
ON CONFLICT (entity_type, entity_id)
DO UPDATE SET generation = policy_outbox.generation + 1;
