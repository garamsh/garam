import {
  Bot,
  FolderGit2,
  LayoutDashboard,
  ScrollText,
  UserRound,
  UsersRound,
  type LucideIcon,
} from "lucide-react";

/** An organization as the console shows it: its slug and name, never its GRN. */
export type ConsoleOrg = { slug: string; name: string };

/** One entry of the org-scoped navigation. */
export type ConsoleSection = {
  /** Path segment below the organization; empty for the overview. */
  segment: string;
  label: string;
  icon: LucideIcon;
};

/** The org-scoped sections, in sidebar and breadcrumb order. */
export const CONSOLE_SECTIONS: readonly ConsoleSection[] = [
  { segment: "", label: "Overview", icon: LayoutDashboard },
  { segment: "groups", label: "Groups", icon: UsersRound },
  { segment: "projects", label: "Projects", icon: FolderGit2 },
  { segment: "agents", label: "Agents", icon: Bot },
  { segment: "members", label: "Members", icon: UserRound },
  { segment: "policies", label: "Policies", icon: ScrollText },
];

/** Where a console pathname sits in the console's structure. */
export type ConsoleLocation =
  | { kind: "orgs" }
  | { kind: "new-org" }
  | { kind: "org"; org: string; section: ConsoleSection }
  | { kind: "invitation" }
  | { kind: "unknown" };

/** A breadcrumb entry; the last one is the current page and has no href. */
export type Crumb = { label: string; href?: string };

/** The URL of a section inside an organization; "" is the overview. */
export function orgHref(org: string, segment: string): string {
  return segment ? `/console/orgs/${org}/${segment}` : `/console/orgs/${org}`;
}

/**
 * Reads the shell's position out of the pathname. The shell renders in
 * a layout, which does not re-render on navigation, so every part that
 * marks the current route derives it from the live pathname instead.
 */
export function parseConsolePath(pathname: string): ConsoleLocation {
  const segments = pathname.split("/").filter(Boolean);
  if (segments[0] !== "console" || segments.length > 4) {
    return { kind: "unknown" };
  }
  if (segments[1] === "invitations") return { kind: "invitation" };
  if (segments[1] !== "orgs") return { kind: "unknown" };
  if (segments.length === 2) return { kind: "orgs" };
  if (segments[2] === "new") return { kind: "new-org" };

  const segment = segments.length === 4 ? segments[3] : "";
  const section = CONSOLE_SECTIONS.find((item) => item.segment === segment);
  if (!section) return { kind: "unknown" };
  return { kind: "org", org: segments[2], section };
}

/** The trail from the organizations list down to the current page. */
export function consoleCrumbs(
  location: ConsoleLocation,
  orgs: readonly ConsoleOrg[],
): Crumb[] {
  const organizations: Crumb = {
    label: "Organizations",
    href: "/console/orgs",
  };
  switch (location.kind) {
    case "orgs":
      return [{ label: organizations.label }];
    case "new-org":
      return [organizations, { label: "New organization" }];
    case "org": {
      // An organization the caller cannot see is not in the list; its
      // slug is the only name the shell has for it.
      const org = orgs.find((candidate) => candidate.slug === location.org);
      return [
        organizations,
        { label: org?.name ?? location.org, href: orgHref(location.org, "") },
        { label: location.section.label },
      ];
    }
    case "invitation":
      return [organizations, { label: "Invitation" }];
    case "unknown":
      return [];
  }
}
