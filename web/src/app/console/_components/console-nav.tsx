"use client";

import { usePathname } from "next/navigation";

import {
  SidebarGroup,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";

import { IntentLink } from "./intent-link";
import {
  CONSOLE_NAV_GROUPS,
  navEntryCurrent,
  orgHref,
  parseConsolePath,
  resolveNavEntry,
  type ConsoleLocation,
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
 * groups `CONSOLE_NAV_GROUPS` declares.
 */
export function ConsoleNav() {
  const location = parseConsolePath(usePathname());
  if (location.kind !== "org") return null;

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
        <SidebarGroup key={group.label} className="py-1">
          {/* The primitive dims its label below AA contrast at this size. */}
          <SidebarGroupLabel className="text-sidebar-foreground">
            {group.label}
          </SidebarGroupLabel>
          <NavItems
            entries={group.entries}
            org={location.org}
            location={location}
            label={group.label}
          />
        </SidebarGroup>
      ))}
    </nav>
  );
}
