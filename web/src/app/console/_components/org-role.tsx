"use client";

import { useOrgShell } from "./org-shell";

/** A member's role in an organization, as garam names it. */
export type OrgRole = "owner" | "admin" | "member";

/** How the switcher names each role. */
export const ROLE_LABEL: Record<OrgRole, string> = {
  owner: "Owner",
  admin: "Admin",
  member: "Member",
};

/**
 * The caller's role in the organization the pathname is inside, once
 * that organization's layout has reported it.
 */
export function useOrgRole(): OrgRole | undefined {
  return useOrgShell()?.role ?? undefined;
}
