import { garamFetchAuthenticated } from "@/lib/garam";
import type { components } from "@/types/api";

export type Organization = components["schemas"]["Organization"];
export type Group = components["schemas"]["Group"];
export type Project = components["schemas"]["Project"];
export type Member = components["schemas"]["Member"];

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

/** Org slugs arrive from the URL, so they are escaped before reuse. */
function orgPath(org: string): string {
  return encodeURIComponent(org);
}
