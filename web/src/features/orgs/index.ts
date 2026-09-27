/**
 * The orgs feature's surface for a Server Component: the organizations
 * a caller can see and what one holds, its policies included; nothing
 * else under `features/orgs/` is public, `Invitation` included — only
 * the invite action reads one. The writes are Server Actions, declared
 * in `actions.ts` — reaching this file from a client module would pull
 * the server-only backend client into the browser bundle.
 */
export {
  getGroup,
  getOrg,
  listAgentDefinitions,
  listAgents,
  listGroupMembers,
  listGroups,
  listOperators,
  listOrgConsents,
  listOrgMembers,
  listOrgs,
  listProjects,
  lookupAgents,
  lookupGroups,
  lookupOrgMembers,
  type Agent,
  type AgentDefinition,
  type Group,
  type GroupMember,
  type Member,
  type Operator,
  type OrgConsent,
  type Organization,
  type Project,
  type RegisteredOperator,
} from "./api/orgs";
export {
  listPolicies,
  listPolicyAttachments,
  listPolicyVersions,
  type Policy,
  type PolicyAttachment,
  type PolicyStatus,
  type PolicyVersion,
} from "./api/policies";
export { parseGrn, slugFromGrn } from "./grn";
