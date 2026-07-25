-- name: CreateSession :exec
INSERT INTO sessions (token_hash, user_grn, created_at, expires_at)
VALUES ($1, $2, $3, $4);

-- name: GetSession :one
SELECT * FROM sessions WHERE token_hash = $1 AND expires_at > now();

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = $1;
