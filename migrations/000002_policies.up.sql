CREATE TABLE policies (
    grn             text PRIMARY KEY,
    org_grn         text NOT NULL REFERENCES orgs (grn) ON DELETE CASCADE,
    name            text NOT NULL,
    status          text NOT NULL CHECK (status IN ('draft', 'active', 'archived')),
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

-- policy_versions is append-only (ADR-0002): a stored version is never
-- rewritten, and it disappears only together with the policy that owns
-- it. A cascade runs after the parent row is gone, which is what
-- distinguishes it from a direct delete.
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

-- Every policy write announces its org so engine caches drop the
-- org's parsed statement set, whichever process performed the write.
CREATE FUNCTION policies_notify_change() RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('gaon_policy_change', coalesce(NEW.org_grn, OLD.org_grn));
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER policies_notify_change
    AFTER INSERT OR UPDATE OR DELETE ON policies
    FOR EACH ROW EXECUTE FUNCTION policies_notify_change();
