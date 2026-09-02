import { notFound } from "next/navigation";
import { z } from "zod";

import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
  parserFor,
} from "@/lib/garam";
import type { components } from "@/types/api";

import { parseGrn } from "../grn";

/** An organization, as the API returns one. */
export type Organization = components["schemas"]["Organization"];
/** A group inside an organization, as the API returns one. */
export type Group = components["schemas"]["Group"];
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
 * An operator an organization has registered, as the API returns one.
 * The record says garam holds one; it says nothing about whether the
 * operator is running or reachable (ADR-0045).
 */
export type Operator = components["schemas"]["Operator"];
/**
 * One cross-organization consent relation, from the side the path
 * named: `orgGrn` is the other organization, and each half says
 * whether that side's grant stands. A relation is a pair and not one
 * organization's switch — messages cross only while both halves stand
 * (ADR-0035).
 */
export type OrgConsent = components["schemas"]["OrgConsent"];

/**
 * What a read returned, or the fact that the API never answered. An
 * empty answer and an unanswered read are different states, and a page
 * that cannot tell them apart reports "none" for an outage.
 */
export type OrgRead<T> = { status: "ok"; data: T } | { status: "unreachable" };

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
const GrnParser = z.string().refine((value) => parseGrn(value) !== null);

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

const GroupParser = parserFor<Group>()(
  z.object({
    grn: GrnParser,
    name: z.string(),
    orgGrn: GrnParser,
    roles: z.array(RoleParser),
  }),
);

const ProjectParser = parserFor<Project>()(
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
        operatorGrn: GrnParser,
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
    operatorGrn: GrnParser,
    values: z.record(z.string(), z.string()),
    definedBy: GrnParser,
    createdAt: z.string(),
  }),
);

export const OperatorParser = parserFor<Operator>()(
  z.object({
    grn: GrnParser,
    orgGrn: GrnParser,
    registeredBy: GrnParser,
    createdAt: z.string(),
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

/** The projects an organization owns. */
export function listProjects(org: string): Promise<OrgRead<Project[]>> {
  return orgRead(
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

/** The cross-organization consent relations an organization is a side of. */
export function listOrgConsents(org: string): Promise<OrgRead<OrgConsent[]>> {
  return orgRead(
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

/** Org slugs arrive from the URL, so they are escaped before reuse. */
function orgPath(org: string): string {
  return encodeURIComponent(org);
}
