"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Building2, Check, ChevronsUpDown, Plus } from "lucide-react";

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
import { cn } from "@/lib/utils";

import { parseConsolePath, type ConsoleOrg } from "./navigation";
import { ROLE_LABEL, useOrgRole } from "./org-role";

/** The first letter of a name, as an organization's tile shows it. */
function initial(name: string): string {
  return Array.from(name.trim())[0]?.toUpperCase() ?? "?";
}

/** An organization's tile: its initial, or a building for none. */
function OrgTile({ name, size }: { name?: string; size: "lg" | "sm" }) {
  return (
    <span
      aria-hidden
      className={cn(
        "flex aspect-square shrink-0 items-center justify-center font-semibold",
        size === "lg"
          ? "size-8 rounded-lg text-sm"
          : "size-5 rounded-md text-[length:var(--text-caption)]",
        name
          ? "bg-sidebar-primary text-sidebar-primary-foreground"
          : "bg-sidebar-accent text-sidebar-foreground",
      )}
    >
      {name ? initial(name) : <Building2 className="size-4" />}
    </span>
  );
}

/**
 * Moves the console between the caller's organizations, and names the
 * one on screen with the caller's role in it once that has been read.
 * Outside an organization it reads as the organizations list. `orgs` is
 * null when the API did not answer: the organization on screen is then
 * named by its slug, and the menu still leads to the list and the
 * create form.
 */
export function OrgSwitcher({ orgs }: { orgs: readonly ConsoleOrg[] | null }) {
  const { isMobile } = useSidebar();
  const location = parseConsolePath(usePathname());
  const activeSlug = location.kind === "org" ? location.org : null;
  const active = orgs?.find((org) => org.slug === activeSlug);
  const role = useOrgRole();
  const degradedSlug = orgs === null ? activeSlug : null;

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
              <OrgTile name={active?.name} size="lg" />
              <span className="grid flex-1 text-left leading-tight">
                {active ? (
                  <>
                    <span className="truncate font-medium text-sidebar-accent-foreground">
                      {active.name}
                    </span>
                    {role && (
                      <span className="truncate text-xs text-muted-foreground">
                        {ROLE_LABEL[role]}
                      </span>
                    )}
                  </>
                ) : degradedSlug ? (
                  <>
                    <span className="truncate font-mono text-xs text-sidebar-accent-foreground">
                      {degradedSlug}
                    </span>
                    <span className="truncate text-xs text-muted-foreground">
                      Can’t load organizations
                    </span>
                  </>
                ) : (
                  <span className="truncate font-medium text-sidebar-accent-foreground">
                    Organizations
                  </span>
                )}
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
            {orgs === null ? (
              <DropdownMenuItem disabled>
                Organizations couldn’t be loaded
              </DropdownMenuItem>
            ) : orgs.length === 0 ? (
              <DropdownMenuItem disabled>No organizations yet</DropdownMenuItem>
            ) : (
              orgs.map((org) => {
                const current = org.slug === activeSlug;
                return (
                  <DropdownMenuItem key={org.slug} asChild>
                    <Link
                      href={`/console/orgs/${org.slug}`}
                      aria-current={current ? "true" : undefined}
                    >
                      <OrgTile name={org.name} size="sm" />
                      <span className="truncate">{org.name}</span>
                      {current && role && (
                        <span className="ml-auto text-xs text-muted-foreground">
                          {ROLE_LABEL[role]}
                        </span>
                      )}
                      {current && <Check className={cn(!role && "ml-auto")} />}
                    </Link>
                  </DropdownMenuItem>
                );
              })
            )}
            <DropdownMenuSeparator />
            <DropdownMenuItem asChild>
              <Link href="/console/orgs">
                <Building2 />
                All organizations
              </Link>
            </DropdownMenuItem>
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
