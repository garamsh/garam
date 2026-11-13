-- What the service records for an organization (ADR-0148): each
-- policy write, kept as long as the organization, and each denial,
-- kept 90 days from its last occurrence. A denial the cap refuses is
-- counted in its day's overflow record.
--
-- `identity` is what makes two denials the same one: a hash of the
-- organization, subject, action, resource and set of policy ids.
-- `bucket` is the hour of a denial's first_at, or the day of an
-- overflow record, counted from the epoch.
CREATE TABLE audit_records (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_grn     text NOT NULL,
    kind        text NOT NULL CHECK (kind IN ('write', 'denial', 'denial-overflow')),
    first_at    timestamptz NOT NULL DEFAULT now(),
    last_at     timestamptz NOT NULL DEFAULT now(),
    count       bigint NOT NULL DEFAULT 1 CHECK (count > 0),
    service     text,
    subject     text,
    action      text,
    resource    text,
    outcome     text,
    policy_ids  text[] NOT NULL DEFAULT '{}',
    version_from integer,
    version_to  integer,
    identity    text,
    bucket      bigint,
    CHECK (
        (kind = 'write'
            AND service IS NOT NULL AND action IS NOT NULL
            AND resource IS NOT NULL
            AND outcome IN ('created', 'version-added', 'activated',
                            'rolled-back', 'removed')
            AND count = 1 AND identity IS NULL AND bucket IS NULL)
        OR (kind = 'denial'
            AND service IS NOT NULL AND subject IS NOT NULL
            AND action IS NOT NULL AND resource IS NOT NULL
            AND outcome = 'deny'
            AND identity IS NOT NULL AND bucket IS NOT NULL)
        OR (kind = 'denial-overflow'
            AND service IS NULL AND subject IS NULL AND action IS NULL
            AND resource IS NULL AND outcome IS NULL
            AND policy_ids = '{}' AND identity IS NULL
            AND bucket IS NOT NULL)
    )
);

-- A denial's hour holds one record per identity, and a day one
-- overflow record, so a concurrent pair cannot both insert.
CREATE UNIQUE INDEX audit_denial_collapse
    ON audit_records (org_grn, identity, bucket) WHERE kind = 'denial';
CREATE UNIQUE INDEX audit_overflow_day
    ON audit_records (org_grn, bucket) WHERE kind = 'denial-overflow';

-- The read pages an organization newest first, and the sweep and the
-- cap's count read by last_at.
CREATE INDEX audit_org_page ON audit_records (org_grn, id DESC);
CREATE INDEX audit_org_last ON audit_records (org_grn, kind, last_at);

-- No record outlives its organization (ADR-0148 item 6): the entity's
-- owner deletes it by writing its tombstone.
CREATE FUNCTION audit_forget_org() RETURNS trigger AS $$
BEGIN
    DELETE FROM audit_records WHERE org_grn = NEW.entity_id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER audit_org_deleted
    AFTER INSERT OR UPDATE ON entities
    FOR EACH ROW
    WHEN (NEW.entity_type = 'Iam::Org' AND NEW.document IS NULL)
    EXECUTE FUNCTION audit_forget_org();
