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

import { parseConsolePath, type ConsoleOrg } from "./navigation";

/** Moves the console between the caller's organizations. */
export function OrgSwitcher({ orgs }: { orgs: ConsoleOrg[] }) {
  const { isMobile } = useSidebar();
  const location = parseConsolePath(usePathname());
  const activeSlug = location.kind === "org" ? location.org : null;
  const active = orgs.find((org) => org.slug === activeSlug);

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
              <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
                <Building2 />
              </div>
              <div className="grid flex-1 text-left text-sm leading-tight">
                <span className="truncate font-medium">
                  {active?.name ?? "Select organization"}
                </span>
                <span className="truncate text-xs text-sidebar-foreground/70">
                  {active?.slug ?? "None selected"}
                </span>
              </div>
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
