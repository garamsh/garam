-- name: CreatePolicy :one
INSERT INTO policies (grn, org_grn, name, status, default_version, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetPolicy :one
SELECT * FROM policies WHERE grn = $1;

-- name: ListPolicies :many
SELECT * FROM policies WHERE org_grn = $1 ORDER BY grn;

-- name: SetPolicyStatus :one
UPDATE policies SET
    status     = sqlc.arg('status'),
    updated_at = sqlc.arg('updated_at')
WHERE grn = sqlc.arg('grn')
RETURNING *;

-- name: SetPolicyDefaultVersion :one
UPDATE policies SET
    default_version = sqlc.arg('default_version'),
    updated_at      = sqlc.arg('updated_at')
WHERE grn = sqlc.arg('grn')
RETURNING *;

-- AddPolicyVersion derives the next version from the rows already
-- stored, so a concurrent append loses the primary key race instead of
-- overwriting a version.
-- name: AddPolicyVersion :one
INSERT INTO policy_versions (policy_grn, version, document, created_by, created_at)
SELECT sqlc.arg('policy_grn'), coalesce(max(version), 0) + 1, sqlc.arg('document'),
       sqlc.arg('created_by'), sqlc.arg('created_at')
FROM policy_versions WHERE policy_grn = sqlc.arg('policy_grn')
RETURNING *;

-- name: CreateFirstPolicyVersion :one
INSERT INTO policy_versions (policy_grn, version, document, created_by, created_at)
VALUES ($1, 1, $2, $3, $4)
RETURNING *;

-- name: GetPolicyVersion :one
SELECT * FROM policy_versions WHERE policy_grn = $1 AND version = $2;

-- name: ListPolicyVersions :many
SELECT * FROM policy_versions WHERE policy_grn = $1 ORDER BY version;

-- name: AttachPolicy :one
INSERT INTO policy_attachments (policy_grn, subject_grn, created_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: DetachPolicy :execrows
DELETE FROM policy_attachments WHERE policy_grn = $1 AND subject_grn = $2;

-- name: ListPolicyAttachments :many
SELECT * FROM policy_attachments WHERE policy_grn = $1 ORDER BY subject_grn;

-- ActivePolicies returns what each active policy of the org currently
-- evaluates: its default version's document, and the subjects it is
-- attached to. An empty subject list means the policy applies org-wide
-- (ADR-0008).
-- name: ActivePolicies :many
SELECT v.document,
       coalesce(array_agg(a.subject_grn) FILTER (WHERE a.subject_grn IS NOT NULL), '{}')::text[] AS subject_grns
FROM policies p
JOIN policy_versions v ON v.policy_grn = p.grn AND v.version = p.default_version
LEFT JOIN policy_attachments a ON a.policy_grn = p.grn
WHERE p.org_grn = $1 AND p.status = 'active'
GROUP BY p.grn, v.document
ORDER BY p.grn;
