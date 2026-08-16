import { notFound } from "next/navigation";

import { GaramApiError, garamFetchAuthenticated } from "@/lib/garam";
import type { components } from "@/types/api";

export type Organization = components["schemas"]["Organization"];
export type Group = components["schemas"]["Group"];
export type Project = components["schemas"]["Project"];
export type Member = components["schemas"]["Member"];
export type Invitation = components["schemas"]["Invitation"];

/** Organizations the signed-in user belongs to. */
export function listOrgs(): Promise<Organization[]> {
  return garamFetchAuthenticated<Organization[]>("/orgs");
}

export function getOrg(org: string): Promise<Organization> {
  return garamFetchAuthenticated<Organization>(`/orgs/${orgPath(org)}`);
}

export function listOrgMembers(org: string): Promise<Member[]> {
  return garamFetchAuthenticated<Member[]>(`/orgs/${orgPath(org)}/members`);
}

export function listGroups(org: string): Promise<Group[]> {
  return garamFetchAuthenticated<Group[]>(`/orgs/${orgPath(org)}/groups`);
}

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
