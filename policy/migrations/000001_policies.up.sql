-- An organization's policies: its role links and guardrails. Semantics
-- are policy_versions' in platform/migrations/000002 (ADR-0002): a
-- version is never rewritten, and it disappears only with the policy
-- that owns it (ADR-0117).
CREATE TABLE policies (
    grn             text PRIMARY KEY,
    org_grn         text NOT NULL,
    name            text NOT NULL,
    status          text NOT NULL CHECK (status IN ('draft', 'active')),
    default_version integer NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX policies_org_status_idx ON policies (org_grn, status);

CREATE TABLE policy_versions (
    policy_grn text NOT NULL REFERENCES policies (grn) ON DELETE CASCADE,
    version    integer NOT NULL,
    body       jsonb NOT NULL,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (policy_grn, version)
);

-- A policy always has the version it decides on. Deferred, because a
-- policy and its first version are written in one transaction.
ALTER TABLE policies
    ADD CONSTRAINT policies_default_version_fkey
    FOREIGN KEY (grn, default_version)
    REFERENCES policy_versions (policy_grn, version)
    DEFERRABLE INITIALLY DEFERRED;

-- A cascade runs after the parent row is gone, which is what tells it
-- from a direct delete.
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
