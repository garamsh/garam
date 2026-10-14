/**
 * The orgs feature's surface for a Server Component: the organizations
 * a caller can see and what one holds, its policies included; nothing
 * else under `features/orgs/` is public, `Invitation` included — only
 * the invite action reads one. The writes are Server Actions, declared
 * in `actions.ts` — reaching this file from a client module would pull
 * the server-only backend client into the browser bundle.
 */
export {
  UNNAMED_AGENT,
  agentChoiceLabel,
  directoryAgents,
  lookUpAgent,
  refusedAgentLists,
  type AgentDirectory,
  type DirectoryAgent,
  type Reported,
} from "./agent-directory";
export {
  getAgentTemplate,
  listAgentTemplates,
  type AgentTemplate,
} from "./api/agent-templates";
export {
  getInvitationPreview,
  joinedOrgPath,
  type InvitationPreview,
  type InvitationPreviewRead,
} from "./api/invitation-preview";
export {
  InvitationSummary,
  invitedSentence,
  unusableInvitation,
} from "./components/invitation-summary";
export {
  getOperatorDelegation,
  hostedFor,
  listHostedOperators,
  type DelegatedOperation,
  type Delegation,
  type DelegationRead,
  type HostedOperator,
} from "./api/delegation";
export {
  getAgentDefinition,
  getGroup,
  getOperator,
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
  lookupAgentDefinitions,
  lookupAgents,
  lookupGroups,
  lookupOperators,
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
  listOperatorKeys,
  type IssuedOperatorKey,
  type OperatorKey,
} from "./api/operator-keys";
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
export {
  KIND_LABEL,
  kindFromGrn,
  type Catalog,
  type Lookup,
  type SubjectKind,
} from "./subject";
