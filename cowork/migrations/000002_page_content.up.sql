-- A page's Yjs document (ADR-0127): the updates clients sent, in the
-- order they arrived, and the snapshot they are compacted into. Both
-- go with the page.
--
-- A snapshot folds in every update through `through`. Loading reads
-- the snapshot and the updates after it in one snapshot of the
-- database, so a compaction between the two reads loses nothing.
CREATE TABLE page_snapshots (
    org      text   NOT NULL,
    page_id  text   NOT NULL,
    snapshot bytea  NOT NULL,
    through  bigint NOT NULL,
    PRIMARY KEY (org, page_id),
    FOREIGN KEY (org, page_id) REFERENCES pages (org, id) ON DELETE CASCADE
);

CREATE TABLE page_updates (
    seq     bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org     text   NOT NULL,
    page_id text   NOT NULL,
    body    bytea  NOT NULL,
    FOREIGN KEY (org, page_id) REFERENCES pages (org, id) ON DELETE CASCADE
);

CREATE INDEX page_updates_by_page ON page_updates (org, page_id, seq);
