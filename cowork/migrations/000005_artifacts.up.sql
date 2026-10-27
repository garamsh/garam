-- An organization's artifacts: HTML pages and documents, each a
-- sequence of immutable versions (ADR-0127 §8). A version's bytes are
-- in the blob store under blob_key; this row is what the store holds
-- and who saved it. Numbers count from 1 per artifact, and latest is
-- the highest.
CREATE TABLE artifacts (
    org        text        NOT NULL,
    id         text        NOT NULL,
    kind       text        NOT NULL,
    title      text        NOT NULL,
    latest     integer     NOT NULL,
    created_by text        NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (org, id),
    CONSTRAINT artifacts_kind CHECK (kind IN ('html', 'document')),
    CONSTRAINT artifacts_title CHECK (octet_length(title) BETWEEN 1 AND 500)
);

CREATE TABLE artifact_versions (
    org          text        NOT NULL,
    artifact_id  text        NOT NULL,
    number       integer     NOT NULL,
    blob_key     text        NOT NULL,
    content_type text        NOT NULL,
    size         bigint      NOT NULL,
    sha256       text        NOT NULL,
    created_by   text        NOT NULL,
    created_at   timestamptz NOT NULL,
    PRIMARY KEY (org, artifact_id, number),
    FOREIGN KEY (org, artifact_id) REFERENCES artifacts (org, id) ON DELETE CASCADE,
    CONSTRAINT artifact_versions_number CHECK (number >= 1)
);

-- A message embeds artifacts by ID, as it links pages.
ALTER TABLE messages ADD COLUMN artifacts text[] NOT NULL DEFAULT '{}';
