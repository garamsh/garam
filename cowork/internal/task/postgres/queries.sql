-- name: InsertTask :exec
INSERT INTO tasks (org, id, title, description, status, assignee, due, created_by, created_at, updated_at)
VALUES (@org, @id, @title, @description, @status, sqlc.narg(assignee), sqlc.narg(due), @created_by, @created_at, @updated_at);

-- name: UpdateTask :execrows
UPDATE tasks
SET title = @title, description = @description, status = @status, assignee = sqlc.narg(assignee),
    due = sqlc.narg(due), updated_at = @updated_at
WHERE org = @org AND id = @id;

-- name: GetTask :one
SELECT * FROM tasks WHERE org = @org AND id = @id;

-- name: ListTasks :many
SELECT t.* FROM tasks t
WHERE t.org = @org
  AND (sqlc.narg(status)::text IS NULL OR t.status = sqlc.narg(status)::text)
  AND (sqlc.narg(assignee)::text IS NULL OR t.assignee = sqlc.narg(assignee)::text)
  AND (sqlc.narg(page)::text IS NULL OR EXISTS (
    SELECT 1 FROM task_links l
    WHERE l.org = t.org AND l.task_id = t.id AND l.kind = 'page' AND l.target = sqlc.narg(page)::text))
  AND (sqlc.narg(message)::text IS NULL OR EXISTS (
    SELECT 1 FROM task_links l
    WHERE l.org = t.org AND l.task_id = t.id AND l.kind = 'message' AND l.target = sqlc.narg(message)::text))
ORDER BY t.created_at DESC, t.id DESC
LIMIT @lim;

-- name: InsertLink :exec
INSERT INTO task_links (org, task_id, kind, target) VALUES (@org, @task_id, @kind, @target);

-- name: DeleteLinks :exec
DELETE FROM task_links WHERE org = @org AND task_id = @task_id;

-- name: ListLinksOf :many
SELECT task_id, kind, target FROM task_links
WHERE org = @org AND task_id = ANY(@tasks::text[])
ORDER BY task_id, kind, target;

-- name: InsertComment :exec
INSERT INTO task_comments (org, id, task_id, author, body, created_at)
VALUES (@org, @id, @task_id, @author, @body, @created_at);

-- name: ListComments :many
SELECT * FROM task_comments WHERE org = @org AND task_id = @task_id ORDER BY created_at, id;
