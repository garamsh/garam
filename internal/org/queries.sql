-- name: CreateOrg :exec
INSERT INTO orgs (grn, name, created_at, updated_at)
VALUES ($1, $2, $3, $4);

-- name: GetOrg :one
SELECT * FROM orgs WHERE grn = $1;

-- name: ListOrgs :many
SELECT * FROM orgs
ORDER BY grn
LIMIT sqlc.narg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: UpdateOrg :one
UPDATE orgs SET
    name       = coalesce(sqlc.narg('name'), name),
    updated_at = sqlc.arg('updated_at')
WHERE grn = sqlc.arg('grn')
RETURNING *;

-- name: DeleteOrg :execrows
DELETE FROM orgs WHERE grn = $1;

-- name: CreateGroup :exec
INSERT INTO groups (grn, org_grn, name, roles, policies, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetGroup :one
SELECT * FROM groups WHERE grn = $1;

-- name: ListGroups :many
SELECT * FROM groups
WHERE org_grn = sqlc.arg('org_grn')
ORDER BY grn
LIMIT sqlc.narg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: UpdateGroup :one
UPDATE groups SET
    name       = coalesce(sqlc.narg('name'), name),
    roles      = coalesce(sqlc.narg('roles')::text[], roles),
    policies   = coalesce(sqlc.narg('policies')::text[], policies),
    updated_at = sqlc.arg('updated_at')
WHERE grn = sqlc.arg('grn')
RETURNING *;

-- name: DeleteGroup :execrows
DELETE FROM groups WHERE grn = $1;

-- name: AddGroupMember :exec
INSERT INTO group_members (group_grn, member_grn) VALUES ($1, $2);

-- name: RemoveGroupMember :exec
DELETE FROM group_members WHERE group_grn = $1 AND member_grn = $2;

-- name: ListGroupMembers :many
SELECT member_grn FROM group_members WHERE group_grn = $1 ORDER BY member_grn;

-- name: ListGroupsByMember :many
SELECT g.grn FROM groups g
JOIN group_members m ON m.group_grn = g.grn
WHERE g.org_grn = sqlc.arg('org_grn') AND m.member_grn = sqlc.arg('member_grn')
ORDER BY g.grn;

-- name: CreateProject :exec
INSERT INTO projects (grn, org_grn, name, roles, policies, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetProject :one
SELECT * FROM projects WHERE grn = $1;

-- name: ListProjects :many
SELECT * FROM projects
WHERE org_grn = sqlc.arg('org_grn')
ORDER BY grn
LIMIT sqlc.narg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: UpdateProject :one
UPDATE projects SET
    name       = coalesce(sqlc.narg('name'), name),
    roles      = coalesce(sqlc.narg('roles')::text[], roles),
    policies   = coalesce(sqlc.narg('policies')::text[], policies),
    updated_at = sqlc.arg('updated_at')
WHERE grn = sqlc.arg('grn')
RETURNING *;

-- name: DeleteProject :execrows
DELETE FROM projects WHERE grn = $1;

-- name: AddProjectMember :exec
INSERT INTO project_members (project_grn, member_grn) VALUES ($1, $2);

-- name: RemoveProjectMember :exec
DELETE FROM project_members WHERE project_grn = $1 AND member_grn = $2;

-- name: ListProjectMembers :many
SELECT member_grn FROM project_members WHERE project_grn = $1 ORDER BY member_grn;

-- name: AddOrgMember :exec
INSERT INTO org_members (org_grn, user_grn, role, created_at)
VALUES ($1, $2, $3, $4);

-- name: GetOrgMember :one
SELECT * FROM org_members WHERE org_grn = $1 AND user_grn = $2;

-- name: ListOrgMembers :many
SELECT * FROM org_members WHERE org_grn = $1 ORDER BY user_grn;

-- name: RemoveOrgMember :exec
DELETE FROM org_members WHERE org_grn = $1 AND user_grn = $2;

-- name: ListOrgsByMember :many
SELECT o.* FROM orgs o
JOIN org_members m ON m.org_grn = o.grn
WHERE m.user_grn = sqlc.arg('user_grn')
ORDER BY o.grn
LIMIT sqlc.narg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: CreateInvitation :exec
INSERT INTO invitations (token, org_grn, role, created_by, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetInvitation :one
SELECT * FROM invitations WHERE token = $1;

-- name: AcceptInvitation :one
UPDATE invitations SET accepted_by = $2, accepted_at = $3
WHERE token = $1 AND accepted_at IS NULL
RETURNING *;
