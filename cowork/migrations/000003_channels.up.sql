-- An organization's channels and their messages (ADR-0127, ADR-0128).
-- A public channel has no participant list. A private channel's list
-- is kept here, and so is a direct message's, fixed when it starts.
-- A list only narrows what the platform allows (ADR-0128).
CREATE TABLE channels (
    org        text        NOT NULL,
    id         text        NOT NULL,
    kind       text        NOT NULL,
    name       text        NOT NULL DEFAULT '',
    created_by text        NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (org, id),
    CONSTRAINT channels_kind CHECK (kind IN ('public', 'private', 'direct')),
    CONSTRAINT channels_name CHECK ((kind = 'direct') = (name = '') AND octet_length(name) <= 80)
);

-- A name is one channel's within the organization; a direct message
-- has none.
CREATE UNIQUE INDEX channels_name_unique ON channels (org, name) WHERE kind <> 'direct';

CREATE TABLE channel_members (
    org        text        NOT NULL,
    channel_id text        NOT NULL,
    member     text        NOT NULL,
    added_at   timestamptz NOT NULL,
    PRIMARY KEY (org, channel_id, member),
    FOREIGN KEY (org, channel_id) REFERENCES channels (org, id) ON DELETE CASCADE
);

CREATE INDEX channel_members_by_member ON channel_members (org, member);

-- A message is the record of the conversation. A reply names the
-- message that starts its thread, which is never itself a reply.
-- `search` is what full-text search reads: the body, in Postgres's
-- language-neutral configuration, since a workspace writes in any.
CREATE TABLE messages (
    org        text        NOT NULL,
    id         text        NOT NULL,
    channel_id text        NOT NULL,
    thread_id  text,
    author     text        NOT NULL,
    body       text        NOT NULL,
    mentions   text[]      NOT NULL DEFAULT '{}',
    pages      text[]      NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL,
    search     tsvector    GENERATED ALWAYS AS (to_tsvector('simple', body)) STORED,
    PRIMARY KEY (org, id),
    FOREIGN KEY (org, channel_id) REFERENCES channels (org, id) ON DELETE CASCADE,
    FOREIGN KEY (org, thread_id) REFERENCES messages (org, id) ON DELETE CASCADE,
    CONSTRAINT messages_body CHECK (octet_length(body) BETWEEN 1 AND 40000)
);

CREATE INDEX messages_by_channel ON messages (org, channel_id, created_at, id) WHERE thread_id IS NULL;
CREATE INDEX messages_by_thread ON messages (org, thread_id, created_at, id) WHERE thread_id IS NOT NULL;
CREATE INDEX messages_search ON messages USING gin (search);
CREATE INDEX messages_pages ON messages USING gin (pages);
