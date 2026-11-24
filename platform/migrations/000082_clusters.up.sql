-- An organization's record of a connected cluster (ADR-0184). garam
-- mints the identifier. The row carries no credential and nothing the
-- cluster reports: the record, its creator and what the organization
-- calls it (ADR-0184 decision 8).
CREATE TABLE clusters (
    grn         text PRIMARY KEY,
    org_grn     text NOT NULL REFERENCES orgs (grn) ON DELETE CASCADE,
    created_by  text NOT NULL,
    created_at  timestamptz NOT NULL,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',

    -- The GRN names a cluster of the org the row belongs to.
    -- internal/cluster derives it so; this is the copy no call path
    -- routes around.
    CONSTRAINT clusters_grn_ownership
        CHECK (grn_service(grn) = 'iam' AND grn_type(grn) = 'cluster'
           AND grn_org(grn) = grn_id(org_grn) AND grn_org(grn) <> ''),
    -- internal/cluster holds the same two bounds, in characters.
    CONSTRAINT clusters_name_length
        CHECK (btrim(name) <> '' AND char_length(name) <= 100),
    CONSTRAINT clusters_description_length
        CHECK (char_length(description) <= 1000)
);

CREATE INDEX clusters_org_idx ON clusters (org_grn);
