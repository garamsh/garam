-- name: GetApp :one
SELECT * FROM apps WHERE grn = $1;

-- An organization sees its own apps and every public one.
-- name: ListApps :many
SELECT * FROM apps
WHERE visibility = 'public' OR org_grn = sqlc.arg('org_grn')
ORDER BY grn;

-- name: CreateInstallation :one
INSERT INTO installations (grn, org_grn, app_grn, scope, project_grns, created_by, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetInstallation :one
SELECT * FROM installations WHERE grn = $1;

-- name: ListInstallations :many
SELECT * FROM installations WHERE org_grn = $1 ORDER BY grn;

-- Taken before an installation is deleted, so a creation of an
-- instance of it either commits first, and its instance is among those
-- DeleteInstallationInstances answers, or waits and finds the
-- installation gone.
-- name: LockInstallation :execrows
SELECT 1 FROM installations WHERE grn = $1 FOR UPDATE;

-- name: DeleteInstallationInstances :many
DELETE FROM instances WHERE installation_grn = $1 RETURNING grn;

-- name: DeleteInstallation :execrows
DELETE FROM installations WHERE grn = $1;

-- Held before an instance of it is written, so two creations of an
-- app's one instance commit one after the other. It answers the app's
-- instance setting; CountInstances, a statement of its own, reads
-- after the lock is granted, so the second creation counts the
-- first's instance. A count in this statement would read the snapshot
-- taken before the lock was waited on.
-- name: HoldInstallation :one
SELECT a.instances
FROM installations n JOIN apps a ON a.grn = n.app_grn
WHERE n.grn = $1
FOR NO KEY UPDATE OF n;

-- name: CountInstances :one
SELECT count(*)::bigint FROM instances WHERE installation_grn = $1;

-- name: CreateInstance :one
INSERT INTO instances (grn, org_grn, installation_grn, created_by, created_at, name, description, kind)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetInstance :one
SELECT * FROM instances WHERE grn = $1;

-- name: ListInstances :many
SELECT * FROM instances WHERE installation_grn = $1 ORDER BY grn;

-- name: GetInstanceContact :many
SELECT last_contact_at FROM instance_contacts WHERE instance_grn = $1;

-- An absent argument keeps the column it would have written, so a
-- caller changing one leaves the others as they stand.
-- name: UpdateInstance :one
UPDATE instances
SET name        = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    kind        = COALESCE(sqlc.narg('kind'), kind)
WHERE grn = sqlc.arg('grn')
RETURNING *;

-- name: DeleteInstance :execrows
DELETE FROM instances WHERE grn = $1;

-- Every credential write holds the instance's row before its keys and
-- tokens, the order an organization's deletion takes them in through
-- the cascades, so neither waits on the other while holding what the
-- other needs (#1424). It answers the database's time, which every
-- check in the transaction reads.
-- name: HoldInstance :one
SELECT now()::timestamptz AS decided_at FROM instances WHERE grn = $1 FOR NO KEY UPDATE;

-- Expired registration tokens of every instance go with each mint and
-- each spend, so no expired hash outlives the next of either.
-- name: SweepRegistrationTokens :exec
DELETE FROM instance_registration_tokens WHERE expires_at <= now();

-- name: IssueRegistrationToken :one
INSERT INTO instance_registration_tokens (instance_grn, token_hash, expires_at)
VALUES (sqlc.arg('instance_grn'), sqlc.arg('token_hash'),
        now() + sqlc.arg('ttl_seconds')::bigint * interval '1 second')
ON CONFLICT (instance_grn) DO UPDATE
SET token_hash = EXCLUDED.token_hash, expires_at = EXCLUDED.expires_at
RETURNING expires_at;

-- Spends the token whatever follows: the row goes in the statement
-- that reads it, so a second presentation finds nothing.
-- name: SpendRegistrationToken :one
DELETE FROM instance_registration_tokens WHERE token_hash = $1
RETURNING instance_grn, expires_at > now() AS live;

-- name: RevokeAllInstanceKeys :exec
UPDATE instance_keys SET revoked_at = now() WHERE instance_grn = $1 AND revoked_at IS NULL;

-- name: DeleteInstanceTokens :exec
DELETE FROM instance_tokens WHERE instance_grn = $1;

-- A key the instance held before is bound again live, its deadlines
-- cleared: registration makes the key sent the record's only live one.
-- name: BindInstanceKey :exec
INSERT INTO instance_keys (instance_grn, kid, public_jwk, created_at)
VALUES (sqlc.arg('instance_grn'), sqlc.arg('kid'), sqlc.arg('public_jwk'), now())
ON CONFLICT (instance_grn, kid) DO UPDATE SET retires_at = NULL, revoked_at = NULL, predecessor_id = NULL;

-- name: GetLiveInstanceKey :one
SELECT k.id, k.instance_grn, k.kid, k.public_jwk, k.created_at, k.retires_at, k.revoked_at
FROM instance_keys k
WHERE k.instance_grn = sqlc.arg('instance_grn') AND k.kid = sqlc.arg('kid')
  AND k.revoked_at IS NULL AND (k.retires_at IS NULL OR k.retires_at > now());

-- name: HoldLiveInstanceKey :one
SELECT id FROM instance_keys
WHERE id = sqlc.arg('id') AND instance_grn = sqlc.arg('instance_grn')
  AND revoked_at IS NULL AND (retires_at IS NULL OR retires_at > now())
FOR NO KEY UPDATE;

-- name: SweepInstanceJTIs :exec
DELETE FROM instance_jtis WHERE instance_grn = $1 AND expires_at <= now();

-- name: SpendInstanceJTI :one
INSERT INTO instance_jtis (instance_grn, jti, expires_at)
VALUES (sqlc.arg('instance_grn'), sqlc.arg('jti'), sqlc.arg('expires_at'))
ON CONFLICT (instance_grn, jti) DO NOTHING
RETURNING jti;

-- name: SweepInstanceTokens :exec
DELETE FROM instance_tokens WHERE instance_grn = $1 AND expires_at <= now();

-- name: RecordInstanceToken :exec
INSERT INTO instance_tokens (token_hash, instance_grn, key_id, issued_at, expires_at)
VALUES (sqlc.arg('token_hash'), sqlc.arg('instance_grn'), sqlc.arg('key_id'),
        sqlc.arg('issued_at'), sqlc.arg('expires_at'));

-- A token that resolves records its instance's contact (REP-5,
-- ADR-0188), skipped while the kept time is under a second old, and
-- answers the machine operations its app lists.
-- name: ResolveInstanceToken :one
WITH resolved AS (
    SELECT t.instance_grn, i.installation_grn, t.expires_at, a.machine_operations, a.requests_bot_hosting
    FROM instance_tokens t
    JOIN instances i ON i.grn = t.instance_grn
    JOIN installations n ON n.grn = i.installation_grn
    JOIN apps a ON a.grn = n.app_grn
    JOIN instance_keys k ON k.id = t.key_id
    WHERE t.token_hash = $1 AND t.expires_at > now()
      AND k.revoked_at IS NULL AND (k.retires_at IS NULL OR k.retires_at > now())
), contact AS (
    INSERT INTO instance_contacts (instance_grn, last_contact_at)
    SELECT instance_grn, now() FROM resolved
    ON CONFLICT (instance_grn) DO UPDATE SET last_contact_at = EXCLUDED.last_contact_at
    WHERE instance_contacts.last_contact_at < EXCLUDED.last_contact_at - interval '1 second'
)
SELECT instance_grn, installation_grn, expires_at, machine_operations, requests_bot_hosting FROM resolved;

-- name: ListInstanceKeys :many
SELECT id, instance_grn, kid, public_jwk, created_at, retires_at, revoked_at, predecessor_id,
       (revoked_at IS NULL AND (retires_at IS NULL OR retires_at > now())) AS live
FROM instance_keys WHERE instance_grn = $1 ORDER BY created_at, id;

-- A rotation starts the predecessor's rollover, leaving an earlier
-- deadline where one is set, and answers the deadline.
-- name: RetireInstanceKey :one
UPDATE instance_keys
SET retires_at = COALESCE(retires_at, now() + sqlc.arg('window_seconds')::bigint * interval '1 second')
WHERE id = sqlc.arg('id')
RETURNING retires_at;

-- name: AddInstanceKey :exec
INSERT INTO instance_keys (instance_grn, kid, public_jwk, created_at, predecessor_id)
VALUES (sqlc.arg('instance_grn'), sqlc.arg('kid'), sqlc.arg('public_jwk'), now(), sqlc.arg('predecessor_id'));

-- name: RevokeInstanceKey :one
UPDATE instance_keys SET revoked_at = now()
WHERE instance_grn = sqlc.arg('instance_grn') AND kid = sqlc.arg('kid') AND revoked_at IS NULL
RETURNING id;

-- name: InstanceKeyExists :one
SELECT EXISTS (SELECT 1 FROM instance_keys WHERE instance_grn = sqlc.arg('instance_grn') AND kid = sqlc.arg('kid'));

-- name: DeleteInstanceKeyTokens :exec
DELETE FROM instance_tokens WHERE key_id = $1;
