-- name: SweepJTIs :exec
DELETE FROM dpop_jtis WHERE expires_at <= now();

-- name: SpendJTI :one
INSERT INTO dpop_jtis (jkt, jti, expires_at)
VALUES (sqlc.arg('jkt'), sqlc.arg('jti'), now() + sqlc.arg('ttl')::interval)
ON CONFLICT (jkt, jti) DO NOTHING
RETURNING jti;

-- name: SweepNonces :exec
DELETE FROM dpop_nonces WHERE expires_at <= now();

-- name: IssueNonce :exec
INSERT INTO dpop_nonces (nonce, expires_at)
VALUES (sqlc.arg('nonce'), now() + sqlc.arg('ttl')::interval);

-- name: NonceLive :one
SELECT EXISTS (SELECT 1 FROM dpop_nonces WHERE nonce = $1 AND expires_at > now());
