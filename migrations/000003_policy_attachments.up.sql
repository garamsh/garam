CREATE TABLE policy_attachments (
    policy_grn  text NOT NULL REFERENCES policies (grn) ON DELETE CASCADE,
    subject_grn text NOT NULL,
    created_at  timestamptz NOT NULL,
    PRIMARY KEY (policy_grn, subject_grn)
);

CREATE INDEX policy_attachments_subject_idx ON policy_attachments (subject_grn);

-- An attachment stays inside the policy's org (ADR-0008). An org-owned
-- subject carries the org identifier in its organization segment
-- (ADR-0001), so the segments must agree; a root-owned identity exists
-- above every org and cannot name one, so it is admitted here and its
-- org membership stays the api server's precondition. A missing policy
-- is left to the foreign key.
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

-- An attachment write changes which subjects an active policy applies
-- to, so it announces the policy's org on the channel policy writes
-- already use. A cascade from the policy finds no row and announces
-- nothing; the policy's own write already did.
CREATE FUNCTION policy_attachments_notify_change() RETURNS trigger AS $$
DECLARE
    org_grn text;
BEGIN
    SELECT p.org_grn INTO org_grn FROM policies p
    WHERE p.grn = coalesce(NEW.policy_grn, OLD.policy_grn);
    IF org_grn IS NOT NULL THEN
        PERFORM pg_notify('gaon_policy_change', org_grn);
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER policy_attachments_notify_change
    AFTER INSERT OR UPDATE OR DELETE ON policy_attachments
    FOR EACH ROW EXECUTE FUNCTION policy_attachments_notify_change();
