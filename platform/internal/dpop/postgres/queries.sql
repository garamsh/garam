-- name: SweepJTIs :exec
DELETE FROM dpop_jtis WHERE expires_at <= now();

-- name: SpendJTI :one
INSERT INTO dpop_jtis (jkt, jti, expires_at)
VALUES (sqlc.arg('jkt'), sqlc.arg('jti'), now() + sqlc.arg('ttl')::interval)
ON CONFLICT (jkt, jti) DO NOTHING
RETURNING jti;

-- A period is the number of whole periods of sqlc.arg('seconds')
-- since the epoch, on the database's clock.

-- name: CurrentNonce :one
SELECT nonce FROM dpop_nonces
WHERE period = floor(extract(epoch FROM now()) / sqlc.arg('seconds')::bigint)::bigint;

-- name: SweepNonces :exec
DELETE FROM dpop_nonces
WHERE period < floor(extract(epoch FROM now()) / sqlc.arg('seconds')::bigint)::bigint - 1;

-- name: KeepNonce :exec
INSERT INTO dpop_nonces (period, nonce)
VALUES (floor(extract(epoch FROM now()) / sqlc.arg('seconds')::bigint)::bigint, sqlc.arg('nonce'))
ON CONFLICT (period) DO NOTHING;

-- name: NonceLive :one
SELECT EXISTS (
    SELECT 1 FROM dpop_nonces
    WHERE nonce = sqlc.arg('nonce')
      AND period >= floor(extract(epoch FROM now()) / sqlc.arg('seconds')::bigint)::bigint - 1
);
