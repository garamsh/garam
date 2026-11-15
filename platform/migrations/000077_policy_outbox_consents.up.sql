-- An organization's consents reach the policy service as the `consents`
-- attribute of its `Iam::Org` (ADR-0147 item 2), and a grant or a
-- withdrawal follows through the outbox. No mark kind is added: a
-- change of a half marks the `Iam::Org` of the organization whose half
-- it is, the entity the attribute belongs to.
--
-- What this does to stored rows: message_consents is read and not
-- written, and policy_outbox keeps every mark. A feed that is already
-- on marks each organization holding a half, below, so a consent given
-- before this migration is delivered.

-- message_consents is locked before policy_outbox, the order a writer
-- of a consent takes (its row, then the mark its trigger writes) and
-- `garam policy-feed start` takes (postgres.go lockSources), so a live
-- writer and this migration wait on each other and never deadlock.
LOCK TABLE message_consents IN SHARE MODE;

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

CREATE TRIGGER policy_outbox_message_consents
    AFTER INSERT OR DELETE OR UPDATE OF a_consented_at, b_consented_at
    ON message_consents
    FOR EACH ROW EXECUTE FUNCTION policy_outbox_track();

-- consent_ids are the organizations toward which the organization the
-- id names holds a half that stands, which is `consents`. The return
-- type gains a column, so the function is dropped and made again.
DROP FUNCTION policy_entity_state(text, text);

CREATE FUNCTION policy_entity_state(p_type text, p_id text)
RETURNS TABLE (org text, members text, parent_types text[], parent_ids text[],
               consent_ids text[])
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
                    FILTER (WHERE p.ptype IS NOT NULL), '{}'::text[]),
           CASE WHEN p_type = 'Iam::Org' THEN
               ARRAY(SELECT c.other FROM consents c ORDER BY c.other)
           ELSE '{}'::text[] END
    FROM self s LEFT JOIN parents p ON true
    GROUP BY s.org, s.members
$$;

-- A feed that is already on was started before consents were marked.
-- Marking each organization that holds a half is what `garam
-- policy-feed start` does for a feed that is off, and a mark already
-- there only has its generation raised.
INSERT INTO policy_outbox (entity_type, entity_id)
SELECT 'Iam::Org', org_a FROM message_consents
    WHERE a_consented_at IS NOT NULL AND (SELECT enabled FROM policy_feed)
UNION
SELECT 'Iam::Org', org_b FROM message_consents
    WHERE b_consented_at IS NOT NULL AND (SELECT enabled FROM policy_feed)
ON CONFLICT (entity_type, entity_id)
DO UPDATE SET generation = policy_outbox.generation + 1;
