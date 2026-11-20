-- The schema as 000080 left it, empty: the rows dropped are not kept.
-- Each object is the one 000002, 000003, 000055 and 000069 left.
CREATE TABLE policies (
    grn             text PRIMARY KEY,
    org_grn         text NOT NULL REFERENCES orgs (grn) ON DELETE CASCADE,
    name            text NOT NULL,
    status          text NOT NULL CHECK (status IN ('draft', 'active')),
    default_version integer NOT NULL,
    created_at      timestamptz NOT NULL,
    updated_at      timestamptz NOT NULL
);

CREATE INDEX policies_org_status_idx ON policies (org_grn, status);

CREATE TABLE policy_versions (
    policy_grn text NOT NULL REFERENCES policies (grn) ON DELETE CASCADE,
    version    integer NOT NULL,
    document   jsonb NOT NULL,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (policy_grn, version)
);

CREATE FUNCTION policy_versions_append_only() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' AND NOT EXISTS (SELECT 1 FROM policies WHERE grn = OLD.policy_grn) THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'policy_versions is append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER policy_versions_append_only
    BEFORE UPDATE OR DELETE ON policy_versions
    FOR EACH ROW EXECUTE FUNCTION policy_versions_append_only();

CREATE FUNCTION policies_notify_change() RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('garam_policy_change', coalesce(NEW.org_grn, OLD.org_grn));
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER policies_notify_change
    AFTER INSERT OR UPDATE OR DELETE ON policies
    FOR EACH ROW EXECUTE FUNCTION policies_notify_change();

CREATE TABLE policy_attachments (
    policy_grn  text NOT NULL REFERENCES policies (grn) ON DELETE CASCADE,
    subject_grn text NOT NULL,
    created_at  timestamptz NOT NULL,
    PRIMARY KEY (policy_grn, subject_grn)
);

CREATE INDEX policy_attachments_subject_idx ON policy_attachments (subject_grn);

CREATE FUNCTION policy_attachments_same_org() RETURNS trigger AS $$
DECLARE
    subject_org text := grn_org(NEW.subject_grn);
    policy_org  text;
BEGIN
    SELECT grn_id(org_grn) INTO policy_org FROM policies WHERE grn = NEW.policy_grn;
    IF policy_org IS NOT NULL AND subject_org <> 'root' AND subject_org <> policy_org THEN
        RAISE EXCEPTION 'policy attachment crosses orgs';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER policy_attachments_same_org
    BEFORE INSERT OR UPDATE ON policy_attachments
    FOR EACH ROW EXECUTE FUNCTION policy_attachments_same_org();

CREATE FUNCTION policy_attachments_notify_change() RETURNS trigger AS $$
DECLARE
    org_grn text;
BEGIN
    SELECT p.org_grn INTO org_grn FROM policies p
    WHERE p.grn = coalesce(NEW.policy_grn, OLD.policy_grn);
    IF org_grn IS NOT NULL THEN
        PERFORM pg_notify('garam_policy_change', org_grn);
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER policy_attachments_notify_change
    AFTER INSERT OR UPDATE OR DELETE ON policy_attachments
    FOR EACH ROW EXECUTE FUNCTION policy_attachments_notify_change();
