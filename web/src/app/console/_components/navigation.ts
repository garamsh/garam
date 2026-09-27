import {
  Bot,
  FolderGit2,
  Handshake,
  LayoutDashboard,
  Server,
  ShieldCheck,
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
  /**
   * Present on a section whose entries each have a page of their own
   * below it: what the breadcrumb calls the create page, and what it
   * calls an entry's page before that page has named the entry.
   */
  items?: { create: string; fallback: string };
  /**
   * Named pages below the section beside its entries, each a view of
   * the section rather than an entry of it.
   */
  views?: readonly { segment: string; label: string }[];
};

/** The segment below an item section that opens its create page. */
export const NEW_ITEM = "new";

/** The org-scoped sections, in sidebar and breadcrumb order. */
export const CONSOLE_SECTIONS: readonly ConsoleSection[] = [
  { segment: "", label: "Overview", icon: LayoutDashboard },
  {
    segment: "teams",
    label: "Teams",
    icon: UsersRound,
    items: { create: "New team", fallback: "Team" },
  },
  { segment: "people", label: "People", icon: UserRound },
  { segment: "agents", label: "Agents", icon: Bot },
  { segment: "projects", label: "Projects", icon: FolderGit2 },
  { segment: "operators", label: "Operators", icon: Server },
  { segment: "consents", label: "Consents", icon: Handshake },
  {
    segment: "permissions",
    label: "Permissions",
    icon: ShieldCheck,
    items: { create: "New policy", fallback: "Policy" },
    views: [{ segment: "attachments", label: "Attachments" }],
  },
];

/**
 * Where a console pathname sits in the console's structure. `item` is
 * the segment below a section that has item pages: `NEW_ITEM` for its
 * create page, a named view's segment, or an entry's identifier.
 */
export type ConsoleLocation =
  | { kind: "orgs" }
  | { kind: "new-org" }
  | { kind: "org"; org: string; section: ConsoleSection; item?: string }
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
  if (segments[0] !== "console" || segments.length > 5) {
    return { kind: "unknown" };
  }
  if (segments[1] === "invitations") return { kind: "invitation" };
  if (segments[1] !== "orgs") return { kind: "unknown" };
  if (segments.length === 2) return { kind: "orgs" };
  if (segments[2] === "new") return { kind: "new-org" };

  const segment = segments.length >= 4 ? segments[3] : "";
  const section = CONSOLE_SECTIONS.find((item) => item.segment === segment);
  if (!section) return { kind: "unknown" };
  if (segments.length === 5) {
    if (!section.items) return { kind: "unknown" };
    return { kind: "org", org: segments[2], section, item: segments[4] };
  }
  return { kind: "org", org: segments[2], section };
}

/**
 * The trail from the organizations list down to the current page.
 * `itemName` is what an item page has named its entry; until it has,
 * the crumb carries the section's fallback rather than the identifier.
 */
export function consoleCrumbs(
  location: ConsoleLocation,
  orgs: readonly ConsoleOrg[],
  itemName?: string,
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
      const trail: Crumb[] = [
        organizations,
        { label: org?.name ?? location.org, href: orgHref(location.org, "") },
      ];
      const { section, item } = location;
      if (item === undefined || !section.items) {
        return [...trail, { label: section.label }];
      }
      const view = section.views?.find((entry) => entry.segment === item);
      return [
        ...trail,
        { label: section.label, href: orgHref(location.org, section.segment) },
        {
          label:
            view?.label ??
            (item === NEW_ITEM
              ? section.items.create
              : (itemName ?? section.items.fallback)),
        },
      ];
    }
    case "invitation":
      return [organizations, { label: "Invitation" }];
    case "unknown":
      return [];
  }
}
