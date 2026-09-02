import { notFound } from "next/navigation";

import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
} from "@/lib/garam";
import type { components } from "@/types/api";

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

/** Organizations the signed-in user belongs to. */
export function listOrgs(): Promise<Organization[]> {
  return garamFetchAuthenticated<Organization[]>("/orgs");
}

/** One organization by slug. */
export function getOrg(org: string): Promise<OrgRead<Organization>> {
  return orgRead(
    garamFetchAuthenticated<Organization>(`/orgs/${orgPath(org)}`),
  );
}

/** The people who belong to an organization. */
export function listOrgMembers(org: string): Promise<OrgRead<Member[]>> {
  return orgRead(
    garamFetchAuthenticated<Member[]>(`/orgs/${orgPath(org)}/members`),
  );
}

/** The groups an organization holds. */
export function listGroups(org: string): Promise<OrgRead<Group[]>> {
  return orgRead(
    garamFetchAuthenticated<Group[]>(`/orgs/${orgPath(org)}/groups`),
  );
}

/** The projects an organization owns. */
export function listProjects(org: string): Promise<OrgRead<Project[]>> {
  return orgRead(
    garamFetchAuthenticated<Project[]>(`/orgs/${orgPath(org)}/projects`),
  );
}

/** The operators an organization has registered. */
export function listOperators(org: string): Promise<OrgRead<Operator[]>> {
  return orgRead(
    garamFetchAuthenticated<Operator[]>(`/orgs/${orgPath(org)}/operators`),
  );
}

/** The agents an organization holds. */
export function listAgents(org: string): Promise<OrgRead<Agent[]>> {
  return orgRead(
    garamFetchAuthenticated<Agent[]>(`/orgs/${orgPath(org)}/agents`),
  );
}

/** The agents an organization says it should have. */
export function listAgentDefinitions(
  org: string,
): Promise<OrgRead<AgentDefinition[]>> {
  return orgRead(
    garamFetchAuthenticated<AgentDefinition[]>(
      `/orgs/${orgPath(org)}/agent-definitions`,
    ),
  );
}

/** The cross-organization consent relations an organization is a side of. */
export function listOrgConsents(org: string): Promise<OrgRead<OrgConsent[]>> {
  return orgRead(
    garamFetchAuthenticated<OrgConsent[]>(`/orgs/${orgPath(org)}/consents`),
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
