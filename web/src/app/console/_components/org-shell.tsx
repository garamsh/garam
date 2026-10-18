"use client";

import { usePathname } from "next/navigation";
import { useEffect, useSyncExternalStore } from "react";

import { parseConsolePath } from "./navigation";
import type { OrgRole } from "./org-role";

/**
 * What the organization's layout read for the shell above it: the
 * caller's role and the sidebar entries the server denies it. A part
 * whose read failed is empty: no role, no entry left out.
 */
export type OrgShell = {
  org: string;
  role: OrgRole | null;
  /** Keys of the sidebar entries whose read the server denied. */
  hidden: readonly string[];
};

// The organization's layout reads these; the sidebar and the palette
// render above that layout and never receive its params. A store
// rather than a context carries them up: a context provider above the
// page re-renders on the report, and a page boundary still streaming
// in then switches to client rendering and leaves the streamed copy in
// the document.
let reported: OrgShell | null = null;
const listeners = new Set<() => void>();

function report(next: OrgShell | null) {
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
 * Reports what the shell needs from `org` while mounted; renders
 * nothing. It writes from an effect, so the store is only ever written
 * in a browser and no request on the server sees another's answers.
 */
export function ReportOrgShell(shell: OrgShell) {
  useEffect(() => {
    report(shell);
    return () => report(null);
    // The props arrive as one serialized object per render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shell.org, shell.role, shell.hidden]);
  return null;
}

/**
 * What the organization the pathname is inside reported, once its
 * layout has. A report for another organization is not this one's, so
 * it is ignored.
 */
export function useOrgShell(): OrgShell | undefined {
  const location = parseConsolePath(usePathname());
  const current = useSyncExternalStore(
    subscribe,
    () => reported,
    () => null,
  );
  if (location.kind !== "org" || current?.org !== location.org) {
    return undefined;
  }
  return current;
}
