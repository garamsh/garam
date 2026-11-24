-- name: CreateCluster :one
INSERT INTO clusters (grn, org_grn, created_by, created_at, name, description)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetCluster :one
SELECT * FROM clusters WHERE grn = $1;

-- name: ListClustersByOrg :many
SELECT * FROM clusters WHERE org_grn = $1 ORDER BY grn;

-- An absent argument keeps the column it would have written, so a
-- caller changing one of the two leaves the other as it stands.
-- name: UpdateClusterDetails :one
UPDATE clusters
SET name        = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description)
WHERE grn = sqlc.arg('grn')
RETURNING *;

-- name: DeleteCluster :execrows
DELETE FROM clusters WHERE grn = $1;

-- Every credential write holds the cluster's row before its keys and
-- tokens, the order an organization's deletion takes them in through
-- the cascades, so neither waits on the other while holding what the
-- other needs (#1424). It answers the database's time, which every
-- check in the transaction reads.
-- name: HoldCluster :one
SELECT now()::timestamptz AS decided_at FROM clusters WHERE grn = $1 FOR NO KEY UPDATE;

-- Expired registration tokens of every cluster go with each mint and
-- each spend, so no expired hash outlives the next of either.
-- name: SweepRegistrationTokens :exec
DELETE FROM cluster_registration_tokens WHERE expires_at <= now();

-- name: IssueRegistrationToken :one
INSERT INTO cluster_registration_tokens (cluster_grn, token_hash, expires_at)
VALUES (sqlc.arg('cluster_grn'), sqlc.arg('token_hash'),
        now() + sqlc.arg('ttl_seconds')::bigint * interval '1 second')
ON CONFLICT (cluster_grn) DO UPDATE
SET token_hash = EXCLUDED.token_hash, expires_at = EXCLUDED.expires_at
RETURNING expires_at;

-- Spends the token whatever follows: the row goes in the statement
-- that reads it, so a second presentation finds nothing.
-- name: SpendRegistrationToken :one
DELETE FROM cluster_registration_tokens WHERE token_hash = $1
RETURNING cluster_grn, expires_at > now() AS live;

-- name: RevokeAllClusterKeys :exec
UPDATE cluster_keys SET revoked_at = now() WHERE cluster_grn = $1 AND revoked_at IS NULL;

-- name: DeleteClusterTokens :exec
DELETE FROM cluster_tokens WHERE cluster_grn = $1;

-- A key the cluster held before is bound again live, its deadlines
-- cleared: registration makes the key sent the record's only live one.
-- name: BindClusterKey :exec
INSERT INTO cluster_keys (cluster_grn, kid, public_jwk, created_at)
VALUES (sqlc.arg('cluster_grn'), sqlc.arg('kid'), sqlc.arg('public_jwk'), now())
ON CONFLICT (cluster_grn, kid) DO UPDATE SET retires_at = NULL, revoked_at = NULL, predecessor_id = NULL;

-- name: GetLiveClusterKey :one
SELECT k.id, k.cluster_grn, k.kid, k.public_jwk, k.created_at, k.retires_at, k.revoked_at
FROM cluster_keys k
WHERE k.cluster_grn = sqlc.arg('cluster_grn') AND k.kid = sqlc.arg('kid')
  AND k.revoked_at IS NULL AND (k.retires_at IS NULL OR k.retires_at > now());

-- name: HoldLiveClusterKey :one
SELECT id FROM cluster_keys
WHERE id = sqlc.arg('id') AND cluster_grn = sqlc.arg('cluster_grn')
  AND revoked_at IS NULL AND (retires_at IS NULL OR retires_at > now())
FOR NO KEY UPDATE;

-- name: SweepClusterJTIs :exec
DELETE FROM cluster_jtis WHERE cluster_grn = $1 AND expires_at <= now();

-- name: SpendClusterJTI :one
INSERT INTO cluster_jtis (cluster_grn, jti, expires_at)
VALUES (sqlc.arg('cluster_grn'), sqlc.arg('jti'), sqlc.arg('expires_at'))
ON CONFLICT (cluster_grn, jti) DO NOTHING
RETURNING jti;

-- name: SweepClusterTokens :exec
DELETE FROM cluster_tokens WHERE cluster_grn = $1 AND expires_at <= now();

-- name: RecordClusterToken :exec
INSERT INTO cluster_tokens (token_hash, cluster_grn, key_id, issued_at, expires_at)
VALUES (sqlc.arg('token_hash'), sqlc.arg('cluster_grn'), sqlc.arg('key_id'),
        sqlc.arg('issued_at'), sqlc.arg('expires_at'));

-- name: ResolveClusterToken :one
SELECT t.cluster_grn, t.expires_at
FROM cluster_tokens t
JOIN clusters c ON c.grn = t.cluster_grn
JOIN cluster_keys k ON k.id = t.key_id
WHERE t.token_hash = $1 AND t.expires_at > now()
  AND k.revoked_at IS NULL AND (k.retires_at IS NULL OR k.retires_at > now());

-- name: ListClusterKeys :many
SELECT id, cluster_grn, kid, public_jwk, created_at, retires_at, revoked_at, predecessor_id,
       (revoked_at IS NULL AND (retires_at IS NULL OR retires_at > now())) AS live
FROM cluster_keys WHERE cluster_grn = $1 ORDER BY created_at, id;

-- A rotation starts the predecessor's rollover, leaving an earlier
-- deadline where one is set, and answers the deadline.
-- name: RetireClusterKey :one
UPDATE cluster_keys
SET retires_at = COALESCE(retires_at, now() + sqlc.arg('window_seconds')::bigint * interval '1 second')
WHERE id = sqlc.arg('id')
RETURNING retires_at;

-- name: AddClusterKey :exec
INSERT INTO cluster_keys (cluster_grn, kid, public_jwk, created_at, predecessor_id)
VALUES (sqlc.arg('cluster_grn'), sqlc.arg('kid'), sqlc.arg('public_jwk'), now(), sqlc.arg('predecessor_id'));

-- name: RevokeClusterKey :one
UPDATE cluster_keys SET revoked_at = now()
WHERE cluster_grn = sqlc.arg('cluster_grn') AND kid = sqlc.arg('kid') AND revoked_at IS NULL
RETURNING id;

-- name: ClusterKeyExists :one
SELECT EXISTS (SELECT 1 FROM cluster_keys WHERE cluster_grn = sqlc.arg('cluster_grn') AND kid = sqlc.arg('kid'));

-- name: DeleteClusterKeyTokens :exec
DELETE FROM cluster_tokens WHERE key_id = $1;
