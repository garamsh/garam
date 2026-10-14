"use client";

import { usePathname } from "next/navigation";
import { Fragment } from "react";

import {
  SidebarGroup,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarSeparator,
} from "@/components/ui/sidebar";

import { IntentLink } from "./intent-link";
import {
  CONSOLE_NAV_GROUPS,
  isUnknownOrg,
  navEntryCurrent,
  orgHref,
  parseConsolePath,
  resolveNavEntry,
  type ConsoleLocation,
  type ConsoleOrg,
  type NavEntry,
} from "./navigation";

function NavItems({
  entries,
  org,
  location,
  label,
}: {
  entries: readonly NavEntry[];
  org: string;
  location: ConsoleLocation;
  label: string;
}) {
  return (
    <SidebarMenu aria-label={label}>
      {entries.map((entry) => {
        const resolved = resolveNavEntry(entry);
        if (!resolved) return null;
        const current = navEntryCurrent(resolved, location);
        return (
          <SidebarMenuItem key={resolved.key}>
            <SidebarMenuButton
              asChild
              isActive={current !== undefined}
              tooltip={resolved.label}
            >
              <IntentLink
                href={orgHref(org, resolved.segment)}
                aria-current={current}
              >
                <resolved.icon />
                <span>{resolved.label}</span>
              </IntentLink>
            </SidebarMenuButton>
          </SidebarMenuItem>
        );
      })}
    </SidebarMenu>
  );
}

/**
 * The sections of the selected organization: the overview, then the
 * groups `CONSOLE_NAV_GROUPS` declares. Nothing is offered for an
 * organization the caller's list does not hold, since every link would
 * lead to the same not-found page; `orgs` is null when the list was not
 * answered, and the sections stay.
 */
export function ConsoleNav({ orgs }: { orgs: readonly ConsoleOrg[] | null }) {
  const location = parseConsolePath(usePathname());
  if (location.kind !== "org" || isUnknownOrg(location, orgs)) return null;

  return (
    <nav aria-label="Organization sections">
      <SidebarGroup className="pb-0">
        <NavItems
          entries={[{ section: "" }]}
          org={location.org}
          location={location}
          label="Overview"
        />
      </SidebarGroup>
      {CONSOLE_NAV_GROUPS.map((group) => (
        <Fragment key={group.label}>
          {/* The collapsed rail hides the labels, so a rule marks where
              one group ends. */}
          <SidebarSeparator className="mx-3 hidden group-data-[collapsible=icon]:block data-horizontal:w-auto" />
          <SidebarGroup className="pt-3 pb-0 group-data-[collapsible=icon]:pt-2">
            <SidebarGroupLabel className="h-7 text-[length:var(--text-caption)] font-medium text-muted-foreground group-data-[collapsible=icon]:-mt-7">
              {group.label}
            </SidebarGroupLabel>
            <NavItems
              entries={group.entries}
              org={location.org}
              location={location}
              label={group.label}
            />
          </SidebarGroup>
        </Fragment>
      ))}
    </nav>
  );
}
