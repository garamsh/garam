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

-- Declarations (ADR-0187). Every write runs inside HoldCluster, so a
-- record's positions are drawn and committed in one order.

-- name: GetDeclarationForWrite :one
SELECT * FROM cluster_declarations WHERE id = $1 FOR UPDATE;

-- An identifier is reserved the first time it is placed, and never
-- released: none answers a row where the identifier was taken before.
-- name: ReserveDeclarationID :execrows
INSERT INTO cluster_declaration_ids (id) VALUES ($1) ON CONFLICT (id) DO NOTHING;

-- name: AdvanceDeclarationHead :one
INSERT INTO cluster_declaration_heads (cluster_grn, head) VALUES ($1, 1)
ON CONFLICT (cluster_grn) DO UPDATE SET head = cluster_declaration_heads.head + 1
RETURNING head;

-- name: WriteDeclaration :one
INSERT INTO cluster_declarations (id, cluster_grn, generation, kind, namespace, name,
                                  object, last_seq, tombstoned, created_at, updated_at)
VALUES (sqlc.arg('id'), sqlc.arg('cluster_grn'), sqlc.arg('generation'), sqlc.arg('kind'),
        sqlc.arg('namespace'), sqlc.arg('name'), sqlc.narg('object'), sqlc.arg('last_seq'),
        sqlc.arg('tombstoned'), sqlc.arg('written_at'), sqlc.arg('written_at'))
ON CONFLICT (id) DO UPDATE
SET generation = EXCLUDED.generation, kind = EXCLUDED.kind, namespace = EXCLUDED.namespace,
    name = EXCLUDED.name, object = EXCLUDED.object, last_seq = EXCLUDED.last_seq,
    tombstoned = EXCLUDED.tombstoned, updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: AppendDeclarationChange :exec
INSERT INTO cluster_declaration_changes (cluster_grn, seq, op, declaration_id, generation, object, written_at)
VALUES (sqlc.arg('cluster_grn'), sqlc.arg('seq'), sqlc.arg('op'), sqlc.arg('declaration_id'),
        sqlc.arg('generation'), sqlc.narg('object'), sqlc.arg('written_at'));

-- The changes past retention go, and the record's purged position
-- moves to the last of them (DECL-6). A record's positions are written
-- in time order, so what goes is a prefix of its log.
-- name: PurgeDeclarationChanges :exec
WITH purged AS (
    DELETE FROM cluster_declaration_changes c
    WHERE c.cluster_grn = sqlc.arg('cluster_grn') AND c.written_at < sqlc.arg('before')
    RETURNING c.seq
)
UPDATE cluster_declaration_heads h
SET purged_through = GREATEST(h.purged_through, (SELECT max(p.seq) FROM purged p))
WHERE h.cluster_grn = sqlc.arg('cluster_grn') AND EXISTS (SELECT 1 FROM purged);

-- name: NotifyDeclarations :exec
SELECT pg_notify('garam_cluster_declarations', sqlc.arg('cluster_grn')::text);

-- A record that wrote nothing stands at position 0, purged through -1.
-- name: GetDeclarationPosition :one
SELECT COALESCE(h.head, 0)::bigint AS head, COALESCE(h.purged_through, -1)::bigint AS purged_through
FROM clusters c LEFT JOIN cluster_declaration_heads h ON h.cluster_grn = c.grn
WHERE c.grn = $1;

-- name: GetLiveDeclaration :one
SELECT * FROM cluster_declarations WHERE cluster_grn = $1 AND id = $2 AND NOT tombstoned;

-- name: ListLiveDeclarations :many
SELECT * FROM cluster_declarations WHERE cluster_grn = $1 AND NOT tombstoned ORDER BY last_seq;

-- name: ListLiveDeclarationsAfter :many
SELECT * FROM cluster_declarations
WHERE cluster_grn = sqlc.arg('cluster_grn') AND NOT tombstoned AND last_seq > sqlc.arg('after')
ORDER BY last_seq LIMIT sqlc.arg('max_rows');

-- name: ListDeclarationChangesAfter :many
SELECT * FROM cluster_declaration_changes
WHERE cluster_grn = sqlc.arg('cluster_grn') AND seq > sqlc.arg('after')
ORDER BY seq LIMIT sqlc.arg('max_rows');
