"use client";

import { MessagesSquare } from "lucide-react";
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
  CONSOLE_NAV,
  isUnknownOrg,
  navEntryCurrent,
  navSection,
  orgHref,
  parseConsolePath,
  type ConsoleLocation,
  type ConsoleOrg,
  type ConsoleSection,
} from "./navigation";
import { useOrgShell } from "./org-shell";

function NavItems({
  entries,
  org,
  location,
  label,
}: {
  entries: readonly ConsoleSection[];
  org: string;
  location: ConsoleLocation;
  label?: string;
}) {
  return (
    <SidebarMenu aria-label={label}>
      {entries.map((resolved) => {
        const current = navEntryCurrent(resolved, location);
        return (
          <SidebarMenuItem key={resolved.segment}>
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

/** "A", "A and B", "A, B and C". */
function listed(labels: readonly string[]): string {
  return labels.length < 2
    ? labels.join("")
    : `${labels.slice(0, -1).join(", ")} and ${labels.at(-1)}`;
}

/** Says once which sections were left out. The collapsed rail has no room. */
function HiddenNote({ labels }: { labels: readonly string[] }) {
  return (
    <p className="px-4 pt-4 text-[length:var(--text-caption)] text-muted-foreground group-data-[collapsible=icon]:hidden">
      {listed(labels)} hidden: no policy lets you read{" "}
      {labels.length === 1 ? "it" : "them"}.
    </p>
  );
}

/**
 * The organization's workspace in cowork, where its people and agents
 * write, message and keep their tasks: another app, so a plain link
 * rather than a section (ADR-0127 §10).
 */
function CoworkLink({ url, org }: { url: string; org: string }) {
  return (
    <SidebarGroup className="pt-3 pb-0 group-data-[collapsible=icon]:pt-2">
      <SidebarMenu>
        <SidebarMenuItem>
          <SidebarMenuButton asChild tooltip="Cowork">
            <a href={`${url.replace(/\/$/, "")}/${encodeURIComponent(org)}`}>
              <MessagesSquare />
              <span>Cowork</span>
            </a>
          </SidebarMenuButton>
        </SidebarMenuItem>
      </SidebarMenu>
    </SidebarGroup>
  );
}

/**
 * The sections of the selected organization, in the groups
 * `CONSOLE_NAV` declares (ADR-0116), and the link to its cowork
 * workspace where `cowork` names where cowork is served. A section whose read the server
 * denies the caller is left out, and the sidebar says so once; until
 * the answer arrives, or where it never does, every section stays.
 * Nothing is offered for an organization the caller's list does not
 * hold, since every link would lead to the same not-found page; `orgs`
 * is null when the list was not answered, and the sections stay.
 */
export function ConsoleNav({
  orgs,
  cowork,
}: {
  orgs: readonly ConsoleOrg[] | null;
  cowork?: string;
}) {
  const location = parseConsolePath(usePathname());
  const hidden = new Set(useOrgShell()?.hidden);
  if (location.kind !== "org" || isUnknownOrg(location, orgs)) return null;

  const groups = CONSOLE_NAV.map((group) => {
    const resolved = group.sections
      .map(navSection)
      .filter((entry) => entry !== undefined);
    return {
      label: group.label,
      key: resolved[0]?.segment ?? "",
      entries: resolved.filter((entry) => !hidden.has(entry.segment)),
      left: resolved.filter((entry) => hidden.has(entry.segment)),
    };
  });
  const left = groups.flatMap((group) => group.left.map((e) => e.label));

  return (
    <nav aria-label="Organization sections">
      {groups
        .filter((group) => group.entries.length > 0)
        .map((group, index) => (
          <Fragment key={group.key}>
            {/* The collapsed rail hides the labels, so a rule marks
                where one group ends. */}
            {index > 0 && (
              <SidebarSeparator className="mx-3 hidden group-data-[collapsible=icon]:block data-horizontal:w-auto" />
            )}
            <SidebarGroup
              className={
                group.label
                  ? "pt-3 pb-0 group-data-[collapsible=icon]:pt-2"
                  : "pb-0"
              }
            >
              {group.label && (
                <SidebarGroupLabel className="h-7 text-[length:var(--text-caption)] font-medium text-muted-foreground group-data-[collapsible=icon]:-mt-7">
                  {group.label}
                </SidebarGroupLabel>
              )}
              <NavItems
                entries={group.entries}
                org={location.org}
                location={location}
                label={group.label}
              />
            </SidebarGroup>
          </Fragment>
        ))}
      {cowork && <CoworkLink url={cowork} org={location.org} />}
      {left.length > 0 && <HiddenNote labels={left} />}
    </nav>
  );
}
