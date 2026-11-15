-- A hosted operator's delegated operations reach the policy service as
-- the `operations` attribute of its `Iam::Operator` (ADR-0146 §A), and
-- the operator sits in its organization's `Iam::OrgMembers` as an agent
-- does, which is what tenant isolation reads of a principal. No mark
-- kind is added: a write or an end of a delegation marks the
-- `Iam::Operator` the delegation names, the entity the attribute
-- belongs to.
--
-- What this does to stored rows: operator_delegations is read and not
-- written, and policy_outbox keeps every mark. A feed that is already
-- on marks each operator holding a current delegation, below, so a
-- delegation written before this migration is delivered.

-- operator_delegations is locked before policy_outbox, the order a
-- writer of a delegation takes (its row, then the mark its trigger
-- writes) and `garam policy-feed start` takes (postgres.go
-- lockSources), so a live writer and this migration wait on each other
-- and never deadlock.
LOCK TABLE operator_delegations IN SHARE MODE;

-- The grant is immutable and ending it is the only update, so the
-- trigger reads an insert, a delete (an operator or an organization
-- deleted takes its delegations) and a change of ended_at. A lapse by
-- expires_at is a clock and writes no row, so it marks nothing.
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
            -- Each organization whose half starts or stops standing
            -- is marked, and one whose half only has its moment
            -- renewed is not. A row deleted with its organization
            -- still holds the halves it had, so the organization that
            -- held one toward it is marked.
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

CREATE TRIGGER policy_outbox_operator_delegations
    AFTER INSERT OR DELETE OR UPDATE OF ended_at
    ON operator_delegations
    FOR EACH ROW EXECUTE FUNCTION policy_outbox_track();

-- operations are the wire names the operator's current delegation
-- holds, which is `operations`: the row with no ended_at, whether or
-- not its expires_at has passed. The return type gains a column, so the
-- function is dropped and made again. An operator also gains its
-- organization's Iam::OrgMembers as a parent.
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

-- A feed that is already on was started before delegations were
-- marked, and delivered each operator without its OrgMembers parent.
-- Marking every operator is what `garam policy-feed start` does for a
-- feed that is off, and a mark already there only has its generation
-- raised.
INSERT INTO policy_outbox (entity_type, entity_id)
SELECT 'Iam::Operator', grn FROM operators WHERE (SELECT enabled FROM policy_feed)
ON CONFLICT (entity_type, entity_id)
DO UPDATE SET generation = policy_outbox.generation + 1;
