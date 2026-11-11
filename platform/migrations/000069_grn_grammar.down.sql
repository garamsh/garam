-- The checks 000069 dropped are not restored: each read a name at the
-- positions of the five-segment form, so on a database in ADR-0142's
-- grammar it would refuse every row it was written for. The trigger
-- and the outbox function are restored as the five-segment form read
-- them.
CREATE FUNCTION policy_attachments_same_org() RETURNS trigger AS $$
DECLARE
    subject_org text := split_part(NEW.subject_grn, ':', 2);
    policy_org  text;
BEGIN
    SELECT split_part(org_grn, ':', 5) INTO policy_org FROM policies WHERE grn = NEW.policy_grn;
    IF policy_org IS NOT NULL AND subject_org <> 'root' AND subject_org <> policy_org THEN
        RAISE EXCEPTION 'policy attachment crosses orgs';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER policy_attachments_same_org
    BEFORE INSERT OR UPDATE ON policy_attachments
    FOR EACH ROW EXECUTE FUNCTION policy_attachments_same_org();

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
            CASE split_part(v_member, ':', 4)
                WHEN 'user' THEN PERFORM policy_outbox_mark('User', v_member);
                WHEN 'agent' THEN PERFORM policy_outbox_mark('Agent', v_member);
                ELSE NULL;
            END CASE;
    END CASE;
    RETURN NULL;
END
$$;
