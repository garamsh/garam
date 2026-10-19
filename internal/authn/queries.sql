-- A session is pruned once nothing it holds is live: its token, and a
-- CLI session's refresh token too (ADR-0120).
-- name: CreateSession :exec
WITH pruned AS (
    DELETE FROM sessions WHERE expires_at <= now()
        AND (refresh_expires_at IS NULL OR refresh_expires_at <= now())
)
INSERT INTO sessions (token_hash, user_grn, created_at, expires_at)
VALUES ($1, $2, $3, $4);

-- name: CreateCLISession :exec
WITH pruned AS (
    DELETE FROM sessions WHERE expires_at <= now()
        AND (refresh_expires_at IS NULL OR refresh_expires_at <= now())
)
INSERT INTO sessions (token_hash, user_grn, created_at, expires_at, kind, name, refresh_hash, refresh_expires_at)
VALUES ($1, $2, $3, $4, 'cli', sqlc.narg('name'), $5, $6);

-- Refreshing replaces both tokens of a live CLI session in one write,
-- so the refresh token it spent buys nothing again.
-- name: RefreshCLISession :one
UPDATE sessions
SET token_hash = sqlc.arg('token_hash'), expires_at = sqlc.arg('expires_at'),
    refresh_hash = sqlc.arg('next_refresh_hash'), refresh_expires_at = sqlc.arg('refresh_expires_at')
WHERE refresh_hash = sqlc.arg('refresh_hash') AND refresh_expires_at > now() AND kind = 'cli'
RETURNING *;

-- name: ListUserSessions :many
SELECT * FROM sessions
WHERE user_grn = $1 AND (expires_at > now() OR refresh_expires_at > now())
ORDER BY created_at DESC, id;

-- name: RevokeUserSession :execrows
DELETE FROM sessions WHERE user_grn = $1 AND id = $2;

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
INSERT INTO oauth_states (state_hash, expires_at, return_path)
SELECT $1, $2, sqlc.narg('return_path')
WHERE (SELECT count(*) FROM oauth_states live WHERE live.expires_at > now()) < sqlc.arg('max_live')::bigint;

-- name: ConsumeOAuthState :one
DELETE FROM oauth_states WHERE state_hash = $1 AND expires_at > now()
RETURNING return_path;

-- name: CreatePasswordCredential :exec
INSERT INTO password_credentials (user_grn, verifier, created_at, updated_at)
VALUES ($1, $2, $3, $4);

-- name: GetPasswordCredential :one
SELECT verifier FROM password_credentials WHERE user_grn = $1;

-- name: ReplacePasswordVerifier :exec
UPDATE password_credentials SET verifier = $2, updated_at = $3 WHERE user_grn = $1;

-- Recording writes nothing once max_live attempts are live, which is
-- what bounds the Argon2id computations an unauthenticated caller can
-- drive on /auth/password/login: the handler hashes nothing when this
-- inserts nothing. The prune runs whether or not the insert does, so a
-- refused attempt still sweeps what lapsed and the table cannot wedge
-- full of dead rows. The count reads the statement's own snapshot, so
-- it sees rows this same statement prunes — the bound it enforces is
-- conservative, never loose. This is ADR-0030's shape applied to a cost
-- that is not a write (ADR-0041).
-- name: RecordPasswordAttempt :execrows
WITH pruned AS (
    DELETE FROM password_attempts WHERE expires_at <= now()
)
INSERT INTO password_attempts (attempt_id, expires_at)
SELECT $1, $2
WHERE (SELECT count(*) FROM password_attempts live WHERE live.expires_at > now()) < sqlc.arg('max_live')::bigint;

-- name: ReleasePasswordAttempt :exec
DELETE FROM password_attempts WHERE attempt_id = $1;

-- Issuing writes nothing once max_live authorizations are live, which
-- bounds the write rate an unauthenticated caller can drive on
-- /auth/device as ADR-0030 bounds /auth/login (ADR-0120). The prune
-- keeps a lapsed row an hour, so a late poll still reads expired_token.
-- name: IssueDeviceAuthorization :execrows
WITH pruned AS (
    DELETE FROM device_authorizations WHERE expires_at <= now() - interval '1 hour'
)
INSERT INTO device_authorizations
    (device_code_hash, user_code_hash, client_name, interval_seconds, created_at, expires_at)
SELECT $1, $2, sqlc.narg('client_name'), $3, now(), now() + make_interval(secs => sqlc.arg('ttl_seconds')::double precision)
WHERE (SELECT count(*) FROM device_authorizations live WHERE live.expires_at > now()) < sqlc.arg('max_live')::bigint;

-- A code is decided once, while it is live.
-- name: DecideDeviceAuthorization :execrows
UPDATE device_authorizations
SET decision = $2, user_grn = $3, decided_at = now()
WHERE user_code_hash = $1 AND decision IS NULL AND expires_at > now();

-- name: LockDeviceAuthorization :one
SELECT device_code_hash, client_name, interval_seconds, decision, user_grn,
       expires_at <= clock_timestamp() AS expired,
       (last_polled_at IS NOT NULL
        AND clock_timestamp() < last_polled_at + make_interval(secs => interval_seconds::double precision)) AS too_early
FROM device_authorizations WHERE device_code_hash = $1
FOR UPDATE;

-- name: TouchDeviceAuthorization :exec
UPDATE device_authorizations SET last_polled_at = clock_timestamp(), interval_seconds = $2
WHERE device_code_hash = $1;

-- name: DeleteDeviceAuthorization :exec
DELETE FROM device_authorizations WHERE device_code_hash = $1;
