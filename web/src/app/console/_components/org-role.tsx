"use client";

import { usePathname } from "next/navigation";
import { useEffect, useSyncExternalStore } from "react";

import { parseConsolePath } from "./navigation";

/** A member's role in an organization, as garam names it. */
export type OrgRole = "owner" | "admin" | "member";

/** How the switcher names each role. */
export const ROLE_LABEL: Record<OrgRole, string> = {
  owner: "Owner",
  admin: "Admin",
  member: "Member",
};

/** The caller's role in one organization, and which one it is. */
type ReportedRole = { org: string; role: OrgRole };

// The organization's layout reads the role; the switcher renders above
// that layout and never receives its params. A store rather than a
// context carries it up: a context provider above the page re-renders
// on the report, and a page boundary still streaming in then switches
// to client rendering and leaves the streamed copy in the document.
let reported: ReportedRole | null = null;
const listeners = new Set<() => void>();

function report(next: ReportedRole | null) {
  reported = next;
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/**
 * Reports the caller's role in `org` while mounted; renders nothing.
 * It writes from an effect, so the store is only ever written in a
 * browser and no request on the server sees another's role.
 */
export function ReportOrgRole({ org, role }: ReportedRole) {
  useEffect(() => {
    report({ org, role });
    return () => report(null);
  }, [org, role]);
  return null;
}

/**
 * The caller's role in the organization the pathname is inside, once
 * that organization's layout has reported it. A role reported for
 * another organization is not this one's, so it is ignored.
 */
export function useOrgRole(): OrgRole | undefined {
  const location = parseConsolePath(usePathname());
  const current = useSyncExternalStore(
    subscribe,
    () => reported,
    () => null,
  );
  if (location.kind !== "org" || current?.org !== location.org) {
    return undefined;
  }
  return current.role;
}
