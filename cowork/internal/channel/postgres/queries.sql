-- name: InsertChannel :exec
INSERT INTO channels (org, id, kind, name, created_by, created_at) VALUES ($1, $2, $3, $4, $5, $6);

-- name: InsertMember :exec
INSERT INTO channel_members (org, channel_id, member, added_at) VALUES ($1, $2, $3, now())
ON CONFLICT DO NOTHING;

-- name: DeleteMember :exec
DELETE FROM channel_members WHERE org = $1 AND channel_id = $2 AND member = $3;

-- name: GetChannel :one
SELECT * FROM channels WHERE org = $1 AND id = $2;

-- name: ListMembers :many
SELECT member FROM channel_members WHERE org = $1 AND channel_id = $2 ORDER BY added_at, member;

-- name: ListChannels :many
-- The public channels, and every channel whose list holds member.
SELECT c.* FROM channels c
WHERE c.org = @org
  AND (c.kind = 'public'
       OR EXISTS (SELECT 1 FROM channel_members m
                  WHERE m.org = c.org AND m.channel_id = c.id AND m.member = @member::text))
ORDER BY c.name, c.id;

-- name: ListMembersOf :many
SELECT channel_id, member FROM channel_members
WHERE org = @org AND channel_id = ANY(@channels::text[])
ORDER BY added_at, member;

-- name: GetMessage :one
SELECT org, id, channel_id, thread_id, author, body, mentions, pages, created_at FROM messages
WHERE org = $1 AND id = $2;

-- name: InsertMessage :exec
INSERT INTO messages (org, id, channel_id, thread_id, author, body, mentions, pages, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: ListMessages :many
-- A channel's messages that are not replies, the last @lim of them
-- before @before (or all, where it is empty), newest first, each with
-- its reply count.
SELECT m.org, m.id, m.channel_id, m.thread_id, m.author, m.body, m.mentions, m.pages, m.created_at,
       (SELECT count(*) FROM messages r WHERE r.org = m.org AND r.thread_id = m.id)::int AS replies
FROM messages m
WHERE m.org = @org AND m.channel_id = @channel_id AND m.thread_id IS NULL
  AND (@before::text = '' OR (m.created_at, m.id) < (SELECT b.created_at, b.id FROM messages b WHERE b.org = @org AND b.id = @before::text))
ORDER BY m.created_at DESC, m.id DESC
LIMIT @lim;

-- name: ListThread :many
SELECT org, id, channel_id, thread_id, author, body, mentions, pages, created_at FROM messages
WHERE org = @org AND (id = @id OR thread_id = @id)
ORDER BY (thread_id IS NOT NULL), created_at, id;

-- name: SearchMessages :many
SELECT org, id, channel_id, thread_id, author, body, mentions, pages, created_at FROM messages
WHERE org = @org AND channel_id = ANY(@channels::text[])
  AND search @@ websearch_to_tsquery('simple', @query::text)
ORDER BY created_at DESC, id DESC
LIMIT @lim;

-- name: ListLinking :many
SELECT org, id, channel_id, thread_id, author, body, mentions, pages, created_at FROM messages
WHERE org = @org AND channel_id = ANY(@channels::text[]) AND pages @> ARRAY[@page::text]
ORDER BY created_at DESC, id DESC
LIMIT @lim;
