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
  return catchUnreachable(
    garamFetchAuthenticated<Organization>(`/orgs/${orgPath(org)}`),
  );
}

/** The people who belong to an organization. */
export function listOrgMembers(org: string): Promise<OrgRead<Member[]>> {
  return catchUnreachable(
    garamFetchAuthenticated<Member[]>(`/orgs/${orgPath(org)}/members`),
  );
}

/** The groups an organization holds. */
export function listGroups(org: string): Promise<OrgRead<Group[]>> {
  return catchUnreachable(
    garamFetchAuthenticated<Group[]>(`/orgs/${orgPath(org)}/groups`),
  );
}

/** The projects an organization owns. */
export function listProjects(org: string): Promise<OrgRead<Project[]>> {
  return catchUnreachable(
    garamFetchAuthenticated<Project[]>(`/orgs/${orgPath(org)}/projects`),
  );
}

/** The operators an organization has registered. */
export function listOperators(org: string): Promise<OrgRead<Operator[]>> {
  return catchUnreachable(
    garamFetchAuthenticated<Operator[]>(`/orgs/${orgPath(org)}/operators`),
  );
}

/** The agents an organization holds. */
export function listAgents(org: string): Promise<OrgRead<Agent[]>> {
  return catchUnreachable(
    garamFetchAuthenticated<Agent[]>(`/orgs/${orgPath(org)}/agents`),
  );
}

/** The agents an organization says it should have. */
export function listAgentDefinitions(
  org: string,
): Promise<OrgRead<AgentDefinition[]>> {
  return catchUnreachable(
    garamFetchAuthenticated<AgentDefinition[]>(
      `/orgs/${orgPath(org)}/agent-definitions`,
    ),
  );
}

/**
 * Rejection handler for an org-scoped read: a denied read and a missing
 * organization are the same answer to a visitor — the organization is
 * not theirs to see.
 */
export function orgNotFound(error: unknown): never {
  if (
    error instanceof GaramApiError &&
    (error.status === 403 || error.status === 404)
  ) {
    notFound();
  }
  throw error;
}

/**
 * Hands an unanswered read to the caller as a state it has to render.
 * Every other rejection travels on: a refusal is `orgNotFound`'s, and a
 * rejected session is already a redirect to the login page.
 */
async function catchUnreachable<T>(read: Promise<T>): Promise<OrgRead<T>> {
  try {
    return { status: "ok", data: await read };
  } catch (error) {
    if (error instanceof GaramUnreachableError) {
      return { status: "unreachable" };
    }
    throw error;
  }
}

/** Org slugs arrive from the URL, so they are escaped before reuse. */
function orgPath(org: string): string {
  return encodeURIComponent(org);
}
