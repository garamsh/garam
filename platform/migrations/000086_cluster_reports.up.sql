-- What a connected cluster tells garam (ADR-0188): the time it last
-- reached garam, and the latest report it sent (cluster-connection
-- v1alpha1 REP-1 to REP-9).

-- The time of a record's latest authenticated request, written when
-- its token resolves. A write is skipped while the kept time is under
-- a second old, so a busy record writes at most once a second.
CREATE TABLE cluster_contacts (
    cluster_grn     text PRIMARY KEY REFERENCES clusters (grn) ON DELETE CASCADE,
    last_contact_at timestamptz NOT NULL
);

-- A record's latest report, the one with the latest observed_at, as
-- garam kept it: only the outcomes and objects naming a declaration
-- placed on the record, or naming none. Every spec, status and
-- message in it is display data (REP-9).
CREATE TABLE cluster_reports (
    cluster_grn text PRIMARY KEY REFERENCES clusters (grn) ON DELETE CASCADE,
    observed_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL,
    report      jsonb NOT NULL
);
