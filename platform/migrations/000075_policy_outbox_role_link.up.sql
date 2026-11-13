-- A member's role reaches the policy service as a role link the
-- platform writes from the membership (ADR-0146 item 4), and the link
-- follows the membership through the outbox. The outbox gains one mark
-- kind for it, `Iam::RoleLink`, whose id is the organization's GRN, a
-- space and the member's GRN: neither holds a space, so the id splits
-- back into the two keys of org_members.
--
-- What this does to stored rows: policy_outbox keeps every mark, and
-- the CHECK only widens. org_members is read and not written, except
-- that a feed that is already on marks each existing membership, below,
-- so a member created before this migration is given its link.

ALTER TABLE policy_outbox DROP CONSTRAINT policy_outbox_entity_type_check;

ALTER TABLE policy_outbox ADD CONSTRAINT policy_outbox_entity_type_check
    CHECK (entity_type IN
        ('Iam::Org', 'Iam::OrgMembers', 'Iam::Team', 'Iam::Project',
         'Iam::User', 'Iam::Agent', 'Iam::Operator', 'Iam::RoleLink'));

-- org_members' branch marks the link on every write of a membership,
-- and an update of its role as well, which marks no entity: the user
-- sits in the same OrgMembers before and after.
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
            PERFORM policy_outbox_mark('Iam::RoleLink',
                (v_row->>'org_grn') || ' ' || (v_row->>'user_grn'));
            IF TG_OP <> 'UPDATE' THEN
                PERFORM policy_outbox_mark('Iam::User', v_row->>'user_grn');
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

CREATE TRIGGER policy_outbox_org_members_role AFTER UPDATE OF role ON org_members
    FOR EACH ROW EXECUTE FUNCTION policy_outbox_track();

-- policy_role_link_state is the surface a delivery reads a link's
-- state through, as policy_entity_state is for an entity: the role the
-- membership the id names holds, and no row once it holds none, which
-- is the link's removal.
--
-- STABLE, so every row it reads comes from one snapshot.
CREATE FUNCTION policy_role_link_state(p_id text) RETURNS TABLE (role text)
LANGUAGE sql STABLE AS $$
    SELECT m.role FROM org_members m
    WHERE m.org_grn = split_part(p_id, ' ', 1)
      AND m.user_grn = split_part(p_id, ' ', 2)
$$;

-- A feed that is already on was started before links were marked, so
-- no existing membership has its link marked. Marking each is what
-- `garam policy-feed start` does for a feed that is off, and a mark
-- that is already there only has its generation raised, so this and a
-- later start overlap harmlessly.
INSERT INTO policy_outbox (entity_type, entity_id)
SELECT 'Iam::RoleLink', org_grn || ' ' || user_grn FROM org_members
WHERE (SELECT enabled FROM policy_feed)
ON CONFLICT (entity_type, entity_id)
DO UPDATE SET generation = policy_outbox.generation + 1;
