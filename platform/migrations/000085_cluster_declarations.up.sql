-- What an organization declares for a connected cluster to run
-- (ADR-0187): the declarations placed on each record, the log of their
-- changes the connector long-polls, and how far that log has been
-- purged (cluster-connection v1alpha1 DECL-1 to DECL-6).

-- Every identifier ever placed. It outlives its declaration and its
-- record, so an identifier names one declaration, once: a report or an
-- object's marker naming it can mean no other.
CREATE TABLE cluster_declaration_ids (
    id text PRIMARY KEY,

    CONSTRAINT cluster_declaration_ids_shape CHECK (id ~ '^[A-Za-z0-9._-]{1,128}$')
);

-- A record's log position. head is the last position written, and
-- purged_through the last position no longer kept: a cursor at or
-- before it is answered 410 (DECL-6). A record with no row has written
-- nothing and purged nothing. A write holds the record's clusters row
-- before this one, so a record's positions commit in the order they
-- were drawn.
CREATE TABLE cluster_declaration_heads (
    cluster_grn    text PRIMARY KEY REFERENCES clusters (grn) ON DELETE CASCADE,
    head           bigint NOT NULL DEFAULT 0,
    purged_through bigint NOT NULL DEFAULT -1,

    CONSTRAINT cluster_declaration_heads_order CHECK (purged_through < head)
);

-- One declaration. A tombstoned one stays while its record does: its
-- generation is where a delete left it. object is the declared object
-- as garam normalized it, and NULL once tombstoned.
CREATE TABLE cluster_declarations (
    id          text PRIMARY KEY REFERENCES cluster_declaration_ids (id),
    cluster_grn text NOT NULL REFERENCES clusters (grn) ON DELETE CASCADE,
    generation  bigint NOT NULL,
    kind        text NOT NULL,
    namespace   text NOT NULL,
    name        text NOT NULL,
    object      jsonb,
    last_seq    bigint NOT NULL,
    tombstoned  boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL,

    CONSTRAINT cluster_declarations_generation CHECK (generation >= 1),
    CONSTRAINT cluster_declarations_object CHECK ((object IS NULL) = tombstoned)
);

-- Two live declarations on one record never name one object.
CREATE UNIQUE INDEX cluster_declarations_live_object_idx
    ON cluster_declarations (cluster_grn, kind, namespace, name) WHERE NOT tombstoned;
-- A full set reads a record's live declarations past last_seq without
-- reading its tombstones; the leading cluster_grn also serves the
-- cascade from clusters, which a partial index would not.
CREATE INDEX cluster_declarations_cluster_idx ON cluster_declarations (cluster_grn, tombstoned, last_seq);

-- Each change, in the order it was written: an apply carrying the
-- whole object, or a delete carrying the identifier alone.
CREATE TABLE cluster_declaration_changes (
    cluster_grn    text NOT NULL REFERENCES clusters (grn) ON DELETE CASCADE,
    seq            bigint NOT NULL,
    op             text NOT NULL,
    declaration_id text NOT NULL,
    generation     bigint NOT NULL,
    object         jsonb,
    written_at     timestamptz NOT NULL,

    PRIMARY KEY (cluster_grn, seq),
    CONSTRAINT cluster_declaration_changes_op CHECK (op IN ('apply', 'delete')),
    CONSTRAINT cluster_declaration_changes_object CHECK ((object IS NULL) = (op = 'delete'))
);

CREATE INDEX cluster_declaration_changes_written_at_idx ON cluster_declaration_changes (cluster_grn, written_at);
