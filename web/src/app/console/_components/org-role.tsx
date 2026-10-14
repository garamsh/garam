"use client";

import { usePathname } from "next/navigation";
import {
  createContext,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react";

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

const OrgRoleContext = createContext<{
  reported: ReportedRole | null;
  setReported: (role: ReportedRole | null) => void;
} | null>(null);

/**
 * Carries the caller's role in the organization on screen up to the
 * sidebar. The organization's own layout reads it; the shell renders
 * above that layout and never receives its params, so the read is
 * handed up the way an item page hands up its breadcrumb name.
 */
export function OrgRoleProvider({ children }: { children: ReactNode }) {
  const [reported, setReported] = useState<ReportedRole | null>(null);
  return (
    <OrgRoleContext.Provider value={{ reported, setReported }}>
      {children}
    </OrgRoleContext.Provider>
  );
}

/** Reports the caller's role in `org`; renders nothing. */
export function ReportOrgRole({ org, role }: ReportedRole) {
  const setReported = useContext(OrgRoleContext)?.setReported;
  useEffect(() => {
    setReported?.({ org, role });
    return () => setReported?.(null);
  }, [org, role, setReported]);
  return null;
}

/**
 * The caller's role in the organization the pathname is inside, once
 * that organization's layout has reported it. A role reported for
 * another organization is not this one's, so it is ignored.
 */
export function useOrgRole(): OrgRole | undefined {
  const location = parseConsolePath(usePathname());
  const reported = useContext(OrgRoleContext)?.reported;
  if (location.kind !== "org" || reported?.org !== location.org) {
    return undefined;
  }
  return reported.role;
}
