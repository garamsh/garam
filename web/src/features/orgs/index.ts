/**
 * The orgs feature's surface for a Server Component: the organizations
 * a caller can see and what one holds; nothing else under
 * `features/orgs/` is public, `Invitation` included — only the invite
 * action reads one. The writes are Server Actions, declared in
 * `actions.ts` — reaching this file from a client module would pull the
 * server-only backend client into the browser bundle.
 */
export {
  getOrg,
  listGroups,
  listOrgMembers,
  listOrgs,
  listProjects,
  orgNotFound,
  type Group,
  type Member,
  type Organization,
  type Project,
} from "./api/orgs";
export { slugFromGrn } from "./grn";
