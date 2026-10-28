-- `groups.org_grn`, `projects.org_grn` and `invitations.org_grn` each
-- lead no index. `ListGroups` and `ListProjects` filter the first two,
-- so each read its whole table to answer for one organization. No query
-- reads the third: the organization-delete cascade reads it, and it
-- scans the whole table to establish that an organization holds no
-- invitation.
--
-- One column each, and it is the predicate. Adding the list's `ORDER BY`
-- column moves the planner to a bitmap scan, which returns no order for
-- that column to have supplied, so the sort stays; and a column distinct
-- per row leaves the repeating leading column nothing to deduplicate, so
-- the index grows by an order of magnitude to hold it.
--
-- `group_members.member_grn` and `project_members.member_grn` were
-- measured beside these three and declined. Neither removes a scan that
-- grows with the deployment — the first reaches one organization's
-- groups through `groups (org_grn)` and is bounded by that, the second
-- is read by no query and carries no foreign key — and indexing all five
-- leaves the `groups_org_grn_fkey` cascade slower than indexing these
-- three alone.
CREATE INDEX groups_org_idx ON groups (org_grn);
CREATE INDEX projects_org_idx ON projects (org_grn);
CREATE INDEX invitations_org_idx ON invitations (org_grn);
