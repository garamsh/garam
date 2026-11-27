-- A declaration write and a bot token's holds the instance's row
-- first, as internal/app's credential writes do, so a record's
-- positions are drawn and committed in one order and no tombstone lands
-- while a bot's token is written. It answers the database's time.
-- name: HoldInstance :one
SELECT now()::timestamptz AS decided_at FROM instances WHERE grn = $1 FOR NO KEY UPDATE;

-- name: InstanceExists :one
SELECT grn FROM instances WHERE grn = $1;

-- Declarations (ADR-0187). Every write runs inside HoldInstance.

-- name: GetDeclarationForWrite :one
SELECT * FROM bot_declarations WHERE id = $1 FOR UPDATE;

-- An identifier is reserved the first time it is placed, and never
-- released: none answers a row where the identifier was taken before.
-- name: ReserveDeclarationID :execrows
INSERT INTO bot_declaration_ids (id) VALUES ($1) ON CONFLICT (id) DO NOTHING;

-- name: AdvanceDeclarationHead :one
INSERT INTO bot_declaration_heads (instance_grn, head) VALUES ($1, 1)
ON CONFLICT (instance_grn) DO UPDATE SET head = bot_declaration_heads.head + 1
RETURNING head;

-- name: WriteDeclaration :one
INSERT INTO bot_declarations (id, instance_grn, generation, kind, namespace, name,
                                  object, last_seq, tombstoned, created_at, updated_at)
VALUES (sqlc.arg('id'), sqlc.arg('instance_grn'), sqlc.arg('generation'), sqlc.arg('kind'),
        sqlc.arg('namespace'), sqlc.arg('name'), sqlc.narg('object'), sqlc.arg('last_seq'),
        sqlc.arg('tombstoned'), sqlc.arg('written_at'), sqlc.arg('written_at'))
ON CONFLICT (id) DO UPDATE
SET generation = EXCLUDED.generation, kind = EXCLUDED.kind, namespace = EXCLUDED.namespace,
    name = EXCLUDED.name, object = EXCLUDED.object, last_seq = EXCLUDED.last_seq,
    tombstoned = EXCLUDED.tombstoned, updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: AppendDeclarationChange :exec
INSERT INTO bot_declaration_changes (instance_grn, seq, op, declaration_id, generation, object, written_at)
VALUES (sqlc.arg('instance_grn'), sqlc.arg('seq'), sqlc.arg('op'), sqlc.arg('declaration_id'),
        sqlc.arg('generation'), sqlc.narg('object'), sqlc.arg('written_at'));

-- The changes past retention go, and the record's purged position
-- moves to the last of them (DECL-6). A record's positions are written
-- in time order, so what goes is a prefix of its log.
-- name: PurgeDeclarationChanges :exec
WITH purged AS (
    DELETE FROM bot_declaration_changes c
    WHERE c.instance_grn = sqlc.arg('instance_grn') AND c.written_at < sqlc.arg('before')
    RETURNING c.seq
)
UPDATE bot_declaration_heads h
SET purged_through = GREATEST(h.purged_through, (SELECT max(p.seq) FROM purged p))
WHERE h.instance_grn = sqlc.arg('instance_grn') AND EXISTS (SELECT 1 FROM purged);

-- name: NotifyDeclarations :exec
SELECT pg_notify('garam_bot_declarations', sqlc.arg('instance_grn')::text);

-- A record that wrote nothing stands at position 0, purged through -1.
-- name: GetDeclarationPosition :one
SELECT COALESCE(h.head, 0)::bigint AS head, COALESCE(h.purged_through, -1)::bigint AS purged_through
FROM instances c LEFT JOIN bot_declaration_heads h ON h.instance_grn = c.grn
WHERE c.grn = $1;

-- name: GetLiveDeclaration :one
SELECT * FROM bot_declarations WHERE instance_grn = $1 AND id = $2 AND NOT tombstoned;

-- name: ListLiveDeclarations :many
SELECT * FROM bot_declarations WHERE instance_grn = $1 AND NOT tombstoned ORDER BY last_seq;

-- name: ListLiveDeclarationsAfter :many
SELECT * FROM bot_declarations
WHERE instance_grn = sqlc.arg('instance_grn') AND NOT tombstoned AND last_seq > sqlc.arg('after')
ORDER BY last_seq LIMIT sqlc.arg('max_rows');

-- name: ListDeclarationChangesAfter :many
SELECT * FROM bot_declaration_changes
WHERE instance_grn = sqlc.arg('instance_grn') AND seq > sqlc.arg('after')
ORDER BY seq LIMIT sqlc.arg('max_rows');

-- Reports (ADR-0188).

-- The identifiers of ids a declaration of the record holds, live or
-- tombstoned (REP-8).
-- name: ListPlacedDeclarationIDs :many
SELECT id FROM bot_declarations
WHERE instance_grn = sqlc.arg('instance_grn') AND id = ANY(sqlc.arg('ids')::text[]);

-- A report replaces the kept one only where it was observed later
-- (REP-5); none answers zero rows.
-- name: KeepReport :execrows
INSERT INTO bot_reports (instance_grn, observed_at, received_at, report)
VALUES (sqlc.arg('instance_grn'), sqlc.arg('observed_at'), now(), sqlc.arg('report'))
ON CONFLICT (instance_grn) DO UPDATE
SET observed_at = EXCLUDED.observed_at, received_at = EXCLUDED.received_at, report = EXCLUDED.report
WHERE bot_reports.observed_at < EXCLUDED.observed_at;

-- name: GetReport :one
SELECT c.grn, r.report
FROM instances c
LEFT JOIN bot_reports r ON r.instance_grn = c.grn
WHERE c.grn = $1;

-- Whether the record's kept report shows a cluster-owned object under
-- kind, namespace and name (gbo ADR 0089 decision 4).
-- name: ReportHolds :one
SELECT EXISTS (
    SELECT 1 FROM bot_reports
    WHERE instance_grn = sqlc.arg('instance_grn') AND report -> 'objects' @> jsonb_build_array(jsonb_build_object(
        'owner', 'cluster', 'kind', sqlc.arg('kind')::text,
        'namespace', sqlc.arg('namespace')::text, 'name', sqlc.arg('name')::text))
) AS held;

-- Bot tokens (ADR-0189).

-- The live Bot declaration that places the bot on the record; none
-- answers zero rows. It takes no lock of its own: RecordBotToken runs
-- inside HoldInstance, the record row's FOR NO KEY UPDATE that every
-- declaration write also takes, so no tombstone lands before the token
-- is written.
-- name: PlacingDeclarationExists :one
SELECT id FROM bot_declarations
WHERE id = sqlc.arg('id') AND instance_grn = sqlc.arg('instance_grn') AND kind = sqlc.arg('kind') AND NOT tombstoned;

-- name: SweepBotTokens :exec
DELETE FROM bot_tokens WHERE instance_grn = $1 AND expires_at <= now();

-- The token goes in under the key the actor's token was exchanged
-- under, read again here, so a key revoked since the actor resolved
-- writes nothing and answers zero rows.
-- name: InsertBotToken :one
INSERT INTO bot_tokens (token_hash, instance_grn, key_id, declaration_id, scope, issued_at, expires_at)
SELECT sqlc.arg('token_hash'), t.instance_grn, t.key_id, sqlc.arg('declaration_id'), sqlc.arg('scope'),
       now(), now() + sqlc.arg('ttl')::interval
FROM instance_tokens t
JOIN instance_keys k ON k.id = t.key_id
WHERE t.token_hash = sqlc.arg('actor_hash') AND t.instance_grn = sqlc.arg('instance_grn')
  AND t.expires_at > now()
  AND k.revoked_at IS NULL AND (k.retires_at IS NULL OR k.retires_at > now())
RETURNING expires_at;

-- name: DeleteDeclarationBotTokens :exec
DELETE FROM bot_tokens WHERE declaration_id = $1;

-- Whether the organization holds an agent record under the GRN
-- ADR-0179 renames into the identifier space a declaration takes.
-- name: AgentRecordExists :one
SELECT EXISTS (SELECT 1 FROM agents WHERE grn = $1) AS held;
