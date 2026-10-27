-- An organization's page tree (ADR-0127). A page is named by its
-- organization and an ID cowork draws; the platform keeps no page and
-- answers what may be done to one by the GRN those two make.
--
-- A page under a parent is deleted with it: a page holds its children,
-- as a folder does. A page at the top has no parent, and the key
-- (org, parent_id) is then not checked.
CREATE TABLE pages (
    org        text        NOT NULL,
    id         text        NOT NULL,
    parent_id  text,
    title      text        NOT NULL DEFAULT '',
    position   integer     NOT NULL,
    created_by text        NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (org, id),
    FOREIGN KEY (org, parent_id) REFERENCES pages (org, id) ON DELETE CASCADE,
    CONSTRAINT pages_not_own_parent CHECK (parent_id IS DISTINCT FROM id),
    CONSTRAINT pages_position CHECK (position >= 0),
    CONSTRAINT pages_title_length CHECK (octet_length(title) <= 500)
);

CREATE INDEX pages_siblings ON pages (org, parent_id, position);
