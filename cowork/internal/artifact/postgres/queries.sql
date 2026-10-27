-- name: InsertArtifact :exec
INSERT INTO artifacts (org, id, kind, title, latest, created_by, created_at, updated_at)
VALUES (@org, @id, @kind, @title, @latest, @created_by, @created_at, @updated_at);

-- name: InsertVersion :exec
INSERT INTO artifact_versions (org, artifact_id, number, blob_key, content_type, size, sha256, created_by, created_at)
VALUES (@org, @artifact_id, @number, @blob_key, @content_type, @size, @sha256, @created_by, @created_at);

-- name: NextVersion :one
-- Takes the artifact's row for the transaction, so two saves at once
-- number one after the other.
UPDATE artifacts SET latest = latest + 1, updated_at = @updated_at
WHERE org = @org AND id = @id
RETURNING *;

-- name: GetArtifact :one
SELECT * FROM artifacts WHERE org = @org AND id = @id;

-- name: ListArtifacts :many
SELECT * FROM artifacts WHERE org = @org ORDER BY updated_at DESC, id DESC LIMIT @lim;

-- name: ListVersions :many
SELECT * FROM artifact_versions WHERE org = @org AND artifact_id = @artifact_id ORDER BY number;

-- name: GetVersion :one
SELECT * FROM artifact_versions WHERE org = @org AND artifact_id = @artifact_id AND number = @number;
