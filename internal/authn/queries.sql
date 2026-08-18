-- name: CreateSession :exec
WITH pruned AS (
    DELETE FROM sessions WHERE expires_at <= now()
)
INSERT INTO sessions (token_hash, user_grn, created_at, expires_at)
VALUES ($1, $2, $3, $4);

-- name: GetSession :one
SELECT * FROM sessions WHERE token_hash = $1 AND expires_at > now();

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = $1;

-- Issuing writes nothing once max_live states are live, which is what
-- bounds the write rate an unauthenticated caller can drive on
-- /auth/login/{provider}: at most max_live rows per flow lifetime,
-- however many processes serve the endpoint. The prune runs whether or
-- not the insert does, so a refused issue still sweeps what lapsed and
-- the table cannot wedge full of dead rows. The count reads the
-- statement's own snapshot, so it sees rows this same statement prunes
-- — the bound it enforces is conservative, never loose.
-- name: IssueOAuthState :execrows
WITH pruned AS (
    DELETE FROM oauth_states WHERE expires_at <= now()
)
INSERT INTO oauth_states (state_hash, expires_at)
SELECT $1, $2
WHERE (SELECT count(*) FROM oauth_states live WHERE live.expires_at > now()) < sqlc.arg('max_live')::bigint;

-- name: ConsumeOAuthState :execrows
DELETE FROM oauth_states WHERE state_hash = $1 AND expires_at > now();
