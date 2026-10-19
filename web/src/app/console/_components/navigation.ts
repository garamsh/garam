import {
  Bot,
  FolderGit2,
  Handshake,
  HardDrive,
  House,
  Server,
  Settings,
  ShieldCheck,
  UserRound,
  UsersRound,
  type LucideIcon,
} from "lucide-react";

/** An organization as the console shows it: its slug and name, never its GRN. */
export type ConsoleOrg = { slug: string; name: string };

/** One entry of the org-scoped navigation. */
export type ConsoleSection = {
  /** Path segment below the organization; empty for Home. */
  segment: string;
  label: string;
  icon: LucideIcon;
  /**
   * The action reading what the section lists. The sidebar leaves the
   * section out where the server denies it; Home has none and is
   * always listed.
   */
  read?: string;
  /**
   * Present on a section whose entries each have a page of their own
   * below it: what the breadcrumb calls the create page, and what it
   * calls an entry's page before that page has named the entry. A
   * section whose identifiers may be `NEW_ITEM` itself has no create
   * page, so that segment stays an entry's.
   */
  items?: {
    create?: string;
    fallback: string;
    /** Pages below an entry's own page, each a segment of its own. */
    pages?: readonly ConsoleItemPage[];
  };
  /**
   * Named pages below the section beside its entries, each a view of
   * the section rather than an entry of it.
   */
  views?: readonly ConsoleView[];
};

/** A named view below a section. */
export type ConsoleView = { segment: string; label: string };

/** A page below one entry's page, such as an operator's setup. */
export type ConsoleItemPage = { segment: string; label: string };

/** The segment below an item section that opens its create page. */
export const NEW_ITEM = "new";

/** The org-scoped sections, in sidebar and breadcrumb order. */
export const CONSOLE_SECTIONS: readonly ConsoleSection[] = [
  { segment: "", label: "Home", icon: House },
  {
    segment: "people",
    label: "People",
    icon: UserRound,
    read: "org:member-list",
    items: { fallback: "Person" },
  },
  {
    segment: "agents",
    label: "Agents",
    icon: Bot,
    read: "agent:read",
    items: { create: "New agent", fallback: "Agent" },
  },
  {
    segment: "teams",
    label: "Teams",
    icon: UsersRound,
    read: "group:read",
    items: { create: "New team", fallback: "Team" },
  },
  {
    segment: "projects",
    label: "Projects",
    icon: FolderGit2,
    read: "project:read",
    items: { fallback: "Project" },
  },
  {
    segment: "operators",
    label: "Operators",
    icon: Server,
    read: "operator:read",
    // An operator's identifier is typed, and `new` is one: registering
    // stays on the list page rather than taking that segment
    // (ADR-0074). `connect` is the operator's setup and `keys` is
    // reserved beside it; both sit below an identifier, so neither
    // takes one (ADR-0074).
    items: {
      fallback: "Operator",
      pages: [{ segment: "connect", label: "Connect" }],
    },
  },
  {
    // Standalone and direct hosts: their approvals and supervisors
    // (ADR-0082, ADR-0111).
    segment: "hosts",
    label: "Hosts",
    icon: HardDrive,
    read: "agent:read",
  },
  {
    // Listed in no sidebar group (ADR-0116): Settings and the palette
    // reach it.
    segment: "consents",
    label: "Consents",
    icon: Handshake,
    read: "consent:read",
  },
  {
    segment: "permissions",
    label: "Permissions",
    icon: ShieldCheck,
    read: "policy:read",
    items: { create: "New policy", fallback: "Policy" },
    views: [{ segment: "attachments", label: "Attachments" }],
  },
  { segment: "settings", label: "Settings", icon: Settings, read: "org:read" },
];

/** A group of sidebar entries, by section segment; one without a label stands alone. */
export type NavGroup = { label?: string; sections: readonly string[] };

/**
 * The sidebar (ADR-0116): Home, Projects, and what the organization
 * holds. A section in no group, such as Consents, is still reached
 * from the palette and by its links.
 */
export const CONSOLE_NAV: readonly NavGroup[] = [
  { sections: [""] },
  { sections: ["projects"] },
  {
    label: "Organization",
    sections: [
      "people",
      "agents",
      "teams",
      "permissions",
      "operators",
      "hosts",
      "settings",
    ],
  },
];

/** A sidebar entry: the section it leads to, found by its segment. */
export function navSection(segment: string): ConsoleSection | undefined {
  return CONSOLE_SECTIONS.find((section) => section.segment === segment);
}

/**
 * Whether a section's entry is where the visitor is: `page` on the
 * section's own page, `location` below it, and undefined elsewhere.
 */
export function navEntryCurrent(
  section: ConsoleSection,
  location: ConsoleLocation,
): "page" | "location" | undefined {
  if (location.kind !== "org" || location.section !== section) {
    return undefined;
  }
  return location.item === undefined ? "page" : "location";
}

/**
 * Where a console pathname sits in the console's structure. `item` is
 * the segment below a section that has item pages: `NEW_ITEM` for its
 * create page, a named view's segment, or an entry's identifier.
 */
export type ConsoleLocation =
  | { kind: "orgs" }
  | { kind: "new-org" }
  | {
      kind: "org";
      org: string;
      section: ConsoleSection;
      item?: string;
      itemPage?: ConsoleItemPage;
    }
  | { kind: "invitation" }
  | { kind: "unknown" };

/** A breadcrumb entry; the last one is the current page and has no href. */
export type Crumb = { label: string; href?: string };

/** The URL of a section inside an organization; "" is Home. */
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
  if (segments[0] !== "console" || segments.length > 6) {
    return { kind: "unknown" };
  }
  if (segments[1] === "invitations") return { kind: "invitation" };
  if (segments[1] !== "orgs") return { kind: "unknown" };
  if (segments.length === 2) return { kind: "orgs" };
  if (segments[2] === "new") return { kind: "new-org" };

  const segment = segments.length >= 4 ? segments[3] : "";
  const section = CONSOLE_SECTIONS.find((item) => item.segment === segment);
  if (!section) return { kind: "unknown" };
  if (segments.length === 6) {
    const isView = section.views?.some(
      (entry) => entry.segment === segments[4],
    );
    const itemPage = section.items?.pages?.find(
      (page) => page.segment === segments[5],
    );
    if (isView || !itemPage) return { kind: "unknown" };
    return {
      kind: "org",
      org: segments[2],
      section,
      item: segments[4],
      itemPage,
    };
  }
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
      const { section, item, itemPage } = location;
      if (item === undefined || !section.items) {
        return [...trail, { label: section.label }];
      }
      const view = section.views?.find((entry) => entry.segment === item);
      const sectionCrumb: Crumb = {
        label: section.label,
        href: orgHref(location.org, section.segment),
      };
      if (itemPage && section.items) {
        return [
          ...trail,
          sectionCrumb,
          {
            label: itemName ?? section.items.fallback,
            href: orgHref(location.org, `${section.segment}/${item}`),
          },
          { label: itemPage.label },
        ];
      }
      return [
        ...trail,
        sectionCrumb,
        {
          label:
            view?.label ??
            (item === NEW_ITEM && section.items.create !== undefined
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

/** What the breadcrumb calls a page that rendered as not found. */
export const NOT_FOUND_LABEL = "Not found";

/**
 * Whether the location is inside an organization the caller's list,
 * read in full, does not hold. A list the API did not answer (`null`)
 * says nothing either way.
 */
export function isUnknownOrg(
  location: ConsoleLocation,
  orgs: readonly ConsoleOrg[] | null,
): boolean {
  return (
    location.kind === "org" &&
    orgs !== null &&
    !orgs.some((org) => org.slug === location.org)
  );
}

/**
 * The trail to a page that does not exist: the crumbs above it that do,
 * then "Not found" in place of a name the page never read. An unknown
 * organization has nothing above it but the organizations list.
 */
export function notFoundCrumbs(
  location: ConsoleLocation,
  orgs: readonly ConsoleOrg[] | null,
): Crumb[] {
  const notFound: Crumb = { label: NOT_FOUND_LABEL };
  if (location.kind !== "org" || isUnknownOrg(location, orgs)) {
    return [{ label: "Organizations", href: "/console/orgs" }, notFound];
  }
  return [...consoleCrumbs(location, orgs ?? []).slice(0, -1), notFound];
}
