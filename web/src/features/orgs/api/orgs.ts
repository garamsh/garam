import { notFound } from "next/navigation";
import { z } from "zod";

import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
  parserFor,
} from "@/lib/garam";
import type { components } from "@/types/api";

import { parseGrn } from "@/lib/grn";

/** An organization, as the API returns one. */
export type Organization = components["schemas"]["Organization"];
/** A group inside an organization, as the API returns one. */
export type Group = components["schemas"]["Group"];
/**
 * One member of a group, as the API returns one: a GRN and nothing
 * else. It may name a user or an agent, and nothing guarantees it
 * still names either — resolving it is the reader's job.
 */
export type GroupMember = components["schemas"]["MemberRef"];
/** A project inside an organization, as the API returns one. */
export type Project = components["schemas"]["Project"];
/** One person's membership of an organization, as the API returns one. */
export type Member = components["schemas"]["Member"];
/** A pending invitation and its bearer token, as the API returns one. */
export type Invitation = components["schemas"]["Invitation"];
/**
 * An agent an operator provisioned outside garam, as the API returns
 * one. What the operator reports about its pod and what the agent
 * reports about itself are separate facts, and neither substitutes for
 * the other (ADR-0053).
 */
export type Agent = components["schemas"]["Agent"];
/**
 * What an organization says an agent should be, as the API returns one.
 * It is a record and not an agent (ADR-0026): no operator has
 * necessarily acted on it, and nothing here reports whether one has.
 */
export type AgentDefinition = components["schemas"]["AgentDefinition"];
/**
 * An operator an organization has registered, as the API returns one,
 * and where it stands in obtaining a certificate. The record says garam
 * holds one; neither it nor the state says whether the operator is
 * running or reachable (ADR-0045) — `enrolled` is an operator that
 * obtained a certificate and not one that is alive. Its name is what
 * the organization calls it and is not unique; the GRN is its identity
 * (ADR-0074).
 */
export type Operator = components["schemas"]["Operator"];
/**
 * What registering an operator answered: the record, and the one-time
 * token that enrols it. garam stores the token's hash and no route
 * reads one back (ADR-0064), so this answer is the only place this
 * token exists — the operation that mints an operator another one
 * answers that one the same way (ADR-0067).
 */
export type RegisteredOperator = components["schemas"]["RegisteredOperator"];
/** An operator's credential as the API answers it, once (ADR-0100). */
export type OperatorCredential = components["schemas"]["OperatorCredential"];
/**
 * One cross-organization consent relation, from the side the path
 * named: `orgGrn` is the other organization, and each half says
 * whether that side's grant stands. A relation is a pair and not one
 * organization's switch — messages cross only while both halves stand
 * (ADR-0035).
 */
export type OrgConsent = components["schemas"]["OrgConsent"];
/**
 * What deleting an organization would destroy, counted by garam at the
 * moment of the read and unfiltered by what the caller may list. The
 * certificate authority stands apart because it alone cannot be made
 * again (ADR-0065).
 */
export type OrgDeletionPreview = components["schemas"]["OrgDeletionPreview"];

/**
 * What a read returned, or the fact that the API never answered. An
 * empty answer and an unanswered read are different states, and a page
 * that cannot tell them apart reports "none" for an outage.
 */
export type OrgRead<T> = { status: "ok"; data: T } | { status: "unreachable" };

/**
 * What a read inside an organization returned where a refusal is a
 * state of its own rather than not-found: a group, its members, or a
 * list a page only looks names up in. It is for a page that also reads
 * the organization itself, whose refusal stays not-found (ADR-0011),
 * so a refusal here tells nothing about an organization that is not
 * the caller's.
 */
export type RefusableRead<T> = OrgRead<T> | { status: "forbidden" };

// What the seam parses each of this feature's reads against. Every
// parser is tied to the type `api/openapi.yaml` generated, in both
// directions: a field the specification adds, drops or retypes fails
// the typecheck here rather than reaching a page as a cast, and a field
// only the parser demands fails it rather than refusing a valid
// response as a contract violation.

/**
 * A GRN the console can read. Refusing a malformed one at the seam is
 * what makes `slugFromGrn` total everywhere downstream — the field is
 * otherwise a throw at whichever render or Server Action reaches it
 * first.
 */
export const GrnParser = z.string().refine((value) => parseGrn(value) !== null);

const RoleParser = parserFor<components["schemas"]["Role"]>()(
  z.enum(["owner", "admin", "member"]),
);

export const OrganizationParser = parserFor<Organization>()(
  z.object({
    grn: GrnParser,
    name: z.string(),
    createdAt: z.string(),
  }),
);

export const GroupParser = parserFor<Group>()(
  z.object({
    grn: GrnParser,
    name: z.string(),
    orgGrn: GrnParser,
    roles: z.array(RoleParser),
  }),
);

const GroupMemberParser = parserFor<GroupMember>()(
  z.object({ memberGrn: GrnParser }),
);

export const ProjectParser = parserFor<Project>()(
  z.object({
    grn: GrnParser,
    name: z.string(),
    orgGrn: GrnParser,
    roles: z.array(RoleParser),
  }),
);

const MemberParser = parserFor<Member>()(
  z.object({
    userGrn: GrnParser,
    nickname: z.string().optional(),
    role: RoleParser,
    since: z.string(),
  }),
);

export const InvitationParser = parserFor<Invitation>()(
  z.object({
    token: z.string(),
    orgGrn: GrnParser,
    role: RoleParser,
    expiresAt: z.string(),
  }),
);

export const AgentParser = parserFor<Agent>()(
  z.object({
    grn: GrnParser,
    orgGrn: GrnParser,
    provisioningState: z.enum(["pending", "provisioned", "ready", "failed"]),
    runtimeState: z.enum(["unknown", "connected", "disconnected"]),
    assignment: z
      .object({
        assigneeGrn: GrnParser,
        operatorGrn: GrnParser.optional(),
        epoch: z.number(),
        since: z.string(),
      })
      .optional(),
    createdAt: z.string(),
    updatedAt: z.string(),
  }),
);

export const AgentDefinitionParser = parserFor<AgentDefinition>()(
  z.object({
    agentGrn: GrnParser,
    orgGrn: GrnParser,
    name: z.string(),
    operatorGrn: GrnParser,
    values: z.record(z.string(), z.string()),
    definedBy: GrnParser,
    createdAt: z.string(),
    cutover: z.object({ stage: z.enum(["frozen", "switched"]) }).nullable(),
  }),
);

export const OperatorParser = parserFor<Operator>()(
  z.object({
    grn: GrnParser,
    name: z.string(),
    description: z.string(),
    orgGrn: GrnParser,
    registeredBy: GrnParser,
    createdAt: z.string(),
    enrollmentState: z.enum(["enrolled", "pending", "expired"]),
    controlEndpoint: z.string().optional(),
    certificateIssuedAt: z.string().optional(),
  }),
);

export const OperatorCredentialParser = parserFor<OperatorCredential>()(
  z.object({
    credential: z.string().min(1),
    issuedAt: z.string(),
  }),
);

export const RegisteredOperatorParser = parserFor<RegisteredOperator>()(
  z.object({
    operator: OperatorParser,
    enrollment: z.object({
      token: z.string().min(1),
      expiresAt: z.string(),
    }),
    credential: OperatorCredentialParser,
  }),
);

export const OrgConsentParser = parserFor<OrgConsent>()(
  z.object({
    orgGrn: GrnParser,
    grantedHere: z.boolean(),
    grantedThere: z.boolean(),
    stands: z.boolean(),
  }),
);

const CountParser = z.number().int().nonnegative();

const OrgDeletionPreviewParser = parserFor<OrgDeletionPreview>()(
  z.object({
    certificateAuthority: z.boolean(),
    counts: z.object({
      groups: CountParser,
      projects: CountParser,
      members: CountParser,
      invitations: CountParser,
      policies: CountParser,
      agents: CountParser,
      agentDefinitions: CountParser,
      agentTemplates: CountParser,
      operators: CountParser,
      consents: CountParser,
    }),
  }),
);

/** Organizations the signed-in user belongs to. */
export function listOrgs(): Promise<Organization[]> {
  return garamFetchAuthenticated("/orgs", z.array(OrganizationParser));
}

/** One organization by slug. */
export function getOrg(org: string): Promise<OrgRead<Organization>> {
  return orgRead(
    garamFetchAuthenticated(`/orgs/${orgPath(org)}`, OrganizationParser),
  );
}

/**
 * What deleting the organization would destroy. It is authorized on
 * the delete itself (ADR-0065), so a caller who may not delete is
 * refused it, and that refusal is returned rather than not-found.
 */
export function getOrgDeletionPreview(
  org: string,
): Promise<RefusableRead<OrgDeletionPreview>> {
  return refusableRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/deletion-preview`,
      OrgDeletionPreviewParser,
    ),
  );
}

/** The people who belong to an organization. */
export function listOrgMembers(org: string): Promise<OrgRead<Member[]>> {
  return orgRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/members`,
      z.array(MemberParser),
    ),
  );
}

/** The groups an organization holds. */
export function listGroups(org: string): Promise<OrgRead<Group[]>> {
  return orgRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/groups`,
      z.array(GroupParser),
    ),
  );
}

/** One group of an organization, by its identifier. */
export function getGroup(
  org: string,
  group: string,
): Promise<RefusableRead<Group>> {
  return refusableRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/groups/${orgPath(group)}`,
      GroupParser,
    ),
  );
}

/**
 * The groups of an organization, for a page that only looks names up
 * in them: a refusal is returned rather than answered with not-found.
 */
export function lookupGroups(org: string): Promise<RefusableRead<Group[]>> {
  return refusableRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/groups`,
      z.array(GroupParser),
    ),
  );
}

/**
 * The groups the signed-in caller belongs to, in one request
 * (`member=me`), with a refusal as a state of its own.
 */
export function lookupMyGroups(org: string): Promise<RefusableRead<Group[]>> {
  return refusableRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/groups?member=me`,
      z.array(GroupParser),
    ),
  );
}

/** The members of one group of an organization. */
export function listGroupMembers(
  org: string,
  group: string,
): Promise<RefusableRead<GroupMember[]>> {
  return refusableRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/groups/${orgPath(group)}/members`,
      z.array(GroupMemberParser),
    ),
  );
}

/**
 * The people of an organization, for a page that only looks names up
 * in them: a refusal is returned rather than answered with not-found.
 */
export function lookupOrgMembers(
  org: string,
): Promise<RefusableRead<Member[]>> {
  return refusableRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/members`,
      z.array(MemberParser),
    ),
  );
}

/**
 * The agents of an organization, for a page that only looks names up
 * in them: a refusal is returned rather than answered with not-found.
 */
export function lookupAgents(org: string): Promise<RefusableRead<Agent[]>> {
  return refusableRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/agents`,
      z.array(AgentParser),
    ),
  );
}

/** The projects an organization owns. */
export function listProjects(org: string): Promise<OrgRead<Project[]>> {
  return orgRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/projects`,
      z.array(ProjectParser),
    ),
  );
}

/**
 * The projects of an organization, for a page that also reads the
 * organization: a refusal is returned rather than answered with
 * not-found.
 */
export function lookupProjects(org: string): Promise<RefusableRead<Project[]>> {
  return refusableRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/projects`,
      z.array(ProjectParser),
    ),
  );
}

/** The operators an organization has registered. */
export function listOperators(org: string): Promise<OrgRead<Operator[]>> {
  return orgRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/operators`,
      z.array(OperatorParser),
    ),
  );
}

/** One operator of an organization, by its identifier. */
export function getOperator(
  org: string,
  operator: string,
): Promise<RefusableRead<Operator>> {
  return refusableRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/operators/${orgPath(operator)}`,
      OperatorParser,
    ),
  );
}

/**
 * The operators of an organization, for a page that only chooses from
 * them: a refusal is returned rather than answered with not-found.
 */
export function lookupOperators(
  org: string,
): Promise<RefusableRead<Operator[]>> {
  return refusableRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/operators`,
      z.array(OperatorParser),
    ),
  );
}

/** The agents an organization holds. */
export function listAgents(org: string): Promise<OrgRead<Agent[]>> {
  return orgRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/agents`,
      z.array(AgentParser),
    ),
  );
}

/** The agents an organization says it should have. */
export function listAgentDefinitions(
  org: string,
): Promise<OrgRead<AgentDefinition[]>> {
  return orgRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/agent-definitions`,
      z.array(AgentDefinitionParser),
    ),
  );
}

/**
 * The agent definitions of an organization, for a page that only looks
 * in them: a refusal is returned rather than answered with not-found.
 */
export function lookupAgentDefinitions(
  org: string,
): Promise<RefusableRead<AgentDefinition[]>> {
  return refusableRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/agent-definitions`,
      z.array(AgentDefinitionParser),
    ),
  );
}

/**
 * One agent definition of an organization, by its agent's identifier,
 * or null where none names it: an agent a control service created has
 * no definition in garam (#1170).
 */
export async function getAgentDefinition(
  org: string,
  agent: string,
): Promise<RefusableRead<AgentDefinition | null>> {
  try {
    return {
      status: "ok",
      data: await garamFetchAuthenticated(
        `/orgs/${orgPath(org)}/agent-definitions/${orgPath(agent)}`,
        AgentDefinitionParser,
      ),
    };
  } catch (error) {
    if (error instanceof GaramApiError && error.status === 404) {
      return { status: "ok", data: null };
    }
    return refusableRead(Promise.reject(error));
  }
}

/**
 * The cross-organization consent relations an organization is a side
 * of, for a page that also reads the organization: reading them needs
 * `consent:read`, which not every member holds, so a refusal is
 * returned rather than answered with not-found.
 */
export function lookupOrgConsents(
  org: string,
): Promise<RefusableRead<OrgConsent[]>> {
  return refusableRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/consents`,
      z.array(OrgConsentParser),
    ),
  );
}

/**
 * The answers an org-scoped read has, applied by the read itself so
 * that no section can hold a different set. An unanswered read is a
 * state the page renders. A denied read and a missing organization are
 * one answer to a visitor — the organization is not theirs to see — and
 * 404 is what every section gives for it (ADR-0011). Every other
 * rejection travels on: a rejected session is already a redirect to the
 * login page.
 */
async function orgRead<T>(read: Promise<T>): Promise<OrgRead<T>> {
  try {
    return { status: "ok", data: await read };
  } catch (error) {
    if (error instanceof GaramUnreachableError) {
      return { status: "unreachable" };
    }
    if (
      error instanceof GaramApiError &&
      (error.status === 403 || error.status === 404)
    ) {
      notFound();
    }
    throw error;
  }
}

/**
 * `orgRead` where a refusal is returned rather than folded into
 * not-found (`RefusableRead`). A missing resource is still
 * `notFound()`.
 */
export async function refusableRead<T>(
  read: Promise<T>,
): Promise<RefusableRead<T>> {
  try {
    return { status: "ok", data: await read };
  } catch (error) {
    if (error instanceof GaramApiError && error.status === 403) {
      return { status: "forbidden" };
    }
    return orgRead(Promise.reject(error));
  }
}

/**
 * Org and group identifiers arrive from the URL, so they are escaped
 * before reuse.
 */
export function orgPath(org: string): string {
  return encodeURIComponent(org);
}
