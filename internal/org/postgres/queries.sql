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

-- name: CountOrgContents :one
-- What this organization holds of what DeleteOrg's cascade removes,
-- counted in one round trip so the four numbers describe one moment.
-- Each count is of the rows keyed on the organization itself; the rows
-- keyed on those in turn -- group_members, project_members,
-- policy_versions, policy_attachments -- go with their parent and are
-- not counted separately.
SELECT
    (SELECT count(*) FROM groups      g WHERE g.org_grn = $1) AS group_count,
    (SELECT count(*) FROM projects    p WHERE p.org_grn = $1) AS project_count,
    (SELECT count(*) FROM org_members m WHERE m.org_grn = $1) AS member_count,
    (SELECT count(*) FROM invitations i WHERE i.org_grn = $1) AS invitation_count;

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

-- name: RemoveGroupMember :execrows
DELETE FROM group_members WHERE group_grn = $1 AND member_grn = $2;

-- name: ListGroupMembers :many
SELECT member_grn FROM group_members WHERE group_grn = $1 ORDER BY member_grn;

-- name: ListGroupsByMember :many
SELECT g.grn FROM groups g
JOIN group_members m ON m.group_grn = g.grn
WHERE g.org_grn = sqlc.arg('org_grn') AND m.member_grn = sqlc.arg('member_grn')
ORDER BY g.grn;

-- ListMemberGroups pages the org's Groups the member belongs to, in
-- the order ListGroups pages them all.
-- name: ListMemberGroups :many
SELECT g.* FROM groups g
JOIN group_members m ON m.group_grn = g.grn
WHERE g.org_grn = sqlc.arg('org_grn') AND m.member_grn = sqlc.arg('member_grn')
ORDER BY g.grn
LIMIT sqlc.narg('row_limit') OFFSET sqlc.arg('row_offset');

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

-- name: RemoveProjectMember :execrows
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

-- name: RemoveOrgMember :execrows
DELETE FROM org_members WHERE org_grn = $1 AND user_grn = $2;

-- name: ListOrgsByMember :many
SELECT o.* FROM orgs o
JOIN org_members m ON m.org_grn = o.grn
WHERE m.user_grn = sqlc.arg('user_grn')
ORDER BY o.grn
LIMIT sqlc.narg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: CreateInvitation :exec
WITH pruned AS (
    DELETE FROM invitations WHERE expires_at <= now() AND accepted_at IS NULL
)
INSERT INTO invitations (token, org_grn, role, created_by, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetInvitation :one
SELECT * FROM invitations WHERE token = $1;

-- name: AcceptInvitation :one
UPDATE invitations SET accepted_by = $2, accepted_at = $3
WHERE token = $1 AND accepted_at IS NULL AND expires_at > $3
RETURNING *;

-- A claimed invitation has accepted_at set and accepted_by NULL: a
-- registration holds it while the account it is for does not exist
-- yet, since accepted_by references users. Every read takes it as
-- spent. A registration that crashed while holding it leaves it so,
-- which fails closed: an admin issues a new invitation (#1381).
-- name: ClaimInvitation :one
UPDATE invitations SET accepted_at = sqlc.arg('claimed_at')
WHERE token = sqlc.arg('token') AND accepted_at IS NULL
  AND expires_at > sqlc.arg('claimed_at')
RETURNING *;

-- Matches only the claim taken at claimed_at, so it completes no
-- other holder's claim.
-- name: CompleteInvitationClaim :execrows
UPDATE invitations SET accepted_by = sqlc.arg('accepted_by')
WHERE token = sqlc.arg('token') AND accepted_by IS NULL
  AND accepted_at = sqlc.arg('claimed_at');

-- Matches only the claim taken at claimed_at, so it undoes no other
-- holder's claim and never an accepted invitation.
-- name: ReleaseInvitationClaim :execrows
UPDATE invitations SET accepted_at = NULL
WHERE token = sqlc.arg('token') AND accepted_by IS NULL
  AND accepted_at = sqlc.arg('claimed_at');
