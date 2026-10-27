-- An organization's tasks: the record of its work (ADR-0127). An
-- assignee is a person's or an agent's GRN, which the platform keeps;
-- cowork keeps the name alone.
CREATE TABLE tasks (
    org         text        NOT NULL,
    id          text        NOT NULL,
    title       text        NOT NULL,
    description text        NOT NULL DEFAULT '',
    status      text        NOT NULL,
    assignee    text,
    due         date,
    created_by  text        NOT NULL,
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL,
    PRIMARY KEY (org, id),
    CONSTRAINT tasks_status CHECK (status IN ('todo', 'doing', 'done')),
    CONSTRAINT tasks_title CHECK (octet_length(title) BETWEEN 1 AND 500),
    CONSTRAINT tasks_description CHECK (octet_length(description) <= 40000)
);

CREATE INDEX tasks_by_assignee ON tasks (org, assignee) WHERE assignee IS NOT NULL;

CREATE TABLE task_comments (
    org        text        NOT NULL,
    id         text        NOT NULL,
    task_id    text        NOT NULL,
    author     text        NOT NULL,
    body       text        NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (org, id),
    FOREIGN KEY (org, task_id) REFERENCES tasks (org, id) ON DELETE CASCADE,
    CONSTRAINT task_comments_body CHECK (octet_length(body) BETWEEN 1 AND 40000)
);

CREATE INDEX task_comments_by_task ON task_comments (org, task_id, created_at, id);

-- What a task links: pages and messages, by their IDs. A link names
-- an object of the same organization, and outlives it, as a link in a
-- page does; reading one is decided when it is followed.
CREATE TABLE task_links (
    org     text NOT NULL,
    task_id text NOT NULL,
    kind    text NOT NULL,
    target  text NOT NULL,
    PRIMARY KEY (org, task_id, kind, target),
    FOREIGN KEY (org, task_id) REFERENCES tasks (org, id) ON DELETE CASCADE,
    CONSTRAINT task_links_kind CHECK (kind IN ('page', 'message'))
);

CREATE INDEX task_links_by_target ON task_links (org, kind, target);
