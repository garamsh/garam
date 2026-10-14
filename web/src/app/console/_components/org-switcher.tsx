"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Check, ChevronsUpDown, Plus } from "lucide-react";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from "@/components/ui/sidebar";

import { parseConsolePath, type ConsoleOrg } from "./navigation";
import { ROLE_LABEL, useOrgRole } from "./org-role";

/** The first letter of a name, as the organization's tile shows it. */
function initial(name: string): string {
  return Array.from(name.trim())[0]?.toUpperCase() ?? "?";
}

/**
 * Moves the console between the caller's organizations, and names the
 * one on screen with the caller's role in it once that has been read.
 */
export function OrgSwitcher({ orgs }: { orgs: ConsoleOrg[] }) {
  const { isMobile } = useSidebar();
  const location = parseConsolePath(usePathname());
  const activeSlug = location.kind === "org" ? location.org : null;
  const active = orgs.find((org) => org.slug === activeSlug);
  const role = useOrgRole();

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <SidebarMenuButton
              size="lg"
              className="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
            >
              <span className="sr-only">Switch organization</span>
              <span
                aria-hidden
                className="flex aspect-square size-8 shrink-0 items-center justify-center rounded-lg bg-sidebar-primary text-sm font-semibold text-sidebar-primary-foreground"
              >
                {active ? initial(active.name) : "?"}
              </span>
              <span className="grid flex-1 text-left leading-tight">
                <span className="truncate font-medium text-sidebar-accent-foreground">
                  {active?.name ?? "Select organization"}
                </span>
                <span className="truncate text-xs text-sidebar-foreground">
                  {active
                    ? role
                      ? ROLE_LABEL[role]
                      : "Organization"
                    : "None selected"}
                </span>
              </span>
              <ChevronsUpDown className="ml-auto" />
            </SidebarMenuButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent
            className="w-(--radix-dropdown-menu-trigger-width) min-w-56"
            align="start"
            side={isMobile ? "bottom" : "right"}
            sideOffset={4}
          >
            <DropdownMenuLabel className="text-xs text-muted-foreground">
              Organizations
            </DropdownMenuLabel>
            {orgs.length === 0 ? (
              <DropdownMenuItem disabled>No organizations yet</DropdownMenuItem>
            ) : (
              orgs.map((org) => (
                <DropdownMenuItem key={org.slug} asChild>
                  <Link
                    href={`/console/orgs/${org.slug}`}
                    aria-current={org.slug === activeSlug ? "true" : undefined}
                  >
                    <span className="truncate">{org.name}</span>
                    {org.slug === activeSlug && <Check className="ml-auto" />}
                  </Link>
                </DropdownMenuItem>
              ))
            )}
            <DropdownMenuSeparator />
            <DropdownMenuItem asChild>
              <Link href="/console/orgs/new">
                <Plus />
                New organization
              </Link>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}
