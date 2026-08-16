import { notFound } from "next/navigation";

import { GaramApiError, garamFetchAuthenticated } from "@/lib/garam";
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

/** Organizations the signed-in user belongs to. */
export function listOrgs(): Promise<Organization[]> {
  return garamFetchAuthenticated<Organization[]>("/orgs");
}

/** One organization by slug. */
export function getOrg(org: string): Promise<Organization> {
  return garamFetchAuthenticated<Organization>(`/orgs/${orgPath(org)}`);
}

/** The people who belong to an organization. */
export function listOrgMembers(org: string): Promise<Member[]> {
  return garamFetchAuthenticated<Member[]>(`/orgs/${orgPath(org)}/members`);
}

/** The groups an organization holds. */
export function listGroups(org: string): Promise<Group[]> {
  return garamFetchAuthenticated<Group[]>(`/orgs/${orgPath(org)}/groups`);
}

/** The projects an organization owns. */
export function listProjects(org: string): Promise<Project[]> {
  return garamFetchAuthenticated<Project[]>(`/orgs/${orgPath(org)}/projects`);
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

/** Org slugs arrive from the URL, so they are escaped before reuse. */
function orgPath(org: string): string {
  return encodeURIComponent(org);
}
