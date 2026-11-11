-- A GRN names its organization, its service and a path of type and id
-- pairs (ADR-0142), and no SQL reads one: a segment is found by
-- lib/grn, never by split_part. Everything that did is removed here or
-- rewritten to ask a table instead of a string.
--
-- What this does to a database holding a five-segment name
-- (the old colon-separated form): it rewrites no row. A stored name
-- keeps its text. The checks dropped below read segments at the positions the
-- five-segment form gave them, so they would refuse a row in the new
-- form and no longer refuse a row in the old one; after this migration
-- the schema holds neither judgement and the stores' own parse is the
-- only one. A row in the old form is one the policy service, the stores
-- and the evaluator refuse to parse, so it is reached by nothing until
-- it is rewritten by a means outside this migration.

-- Every CHECK of the schema that reads a GRN with split_part: the
-- agent, operator, supervisor, delegation, evidence and audit tables
-- each held one to keep a name's type and organization agreeing with
-- the row's own columns. The set is read off the catalog so that a
-- constraint a later migration renamed or moved is dropped where it
-- stands.
DO $$
DECLARE
    c record;
BEGIN
    FOR c IN
        SELECT conrelid::regclass AS tbl, conname
        FROM pg_constraint
        WHERE contype = 'c'
          AND connamespace = current_schema()::regnamespace
          AND pg_get_constraintdef(oid) LIKE '%split_part%'
    LOOP
        EXECUTE format('ALTER TABLE %s DROP CONSTRAINT %I', c.tbl, c.conname);
    END LOOP;
END
$$;

-- An attachment stays inside the policy's org (ADR-0008). The trigger
-- compared two GRNs' organization segments; the policy server makes
-- the same comparison on the parsed names before it stores one.
DROP TRIGGER policy_attachments_same_org ON policy_attachments;
DROP FUNCTION policy_attachments_same_org();

-- A member of a group or a project is a user or an agent, told by
-- the table that holds it and not by its name. A GRN of any other type
-- marks nothing, as before. A member no table holds marks nothing
-- either: where it was a user or an agent that was deleted, its own
-- table's trigger has marked it.
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
            IF EXISTS (SELECT 1 FROM users WHERE grn = v_member) THEN
                PERFORM policy_outbox_mark('User', v_member);
            ELSIF EXISTS (SELECT 1 FROM agents WHERE grn = v_member) THEN
                PERFORM policy_outbox_mark('Agent', v_member);
            END IF;
    END CASE;
    RETURN NULL;
END
$$;
