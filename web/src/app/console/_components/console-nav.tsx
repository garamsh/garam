"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

import {
  SidebarGroup,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";

import { CONSOLE_SECTIONS, orgHref, parseConsolePath } from "./navigation";

/** The sections of the selected organization. */
export function ConsoleNav() {
  const location = parseConsolePath(usePathname());
  if (location.kind !== "org") return null;

  return (
    <SidebarGroup>
      <SidebarGroupLabel>Organization</SidebarGroupLabel>
      <SidebarMenu aria-label="Organization sections">
        {CONSOLE_SECTIONS.map((section) => {
          const current = section === location.section;
          return (
            <SidebarMenuItem key={section.label}>
              <SidebarMenuButton
                asChild
                isActive={current}
                tooltip={section.label}
              >
                <Link
                  href={orgHref(location.org, section.segment)}
                  aria-current={current ? "page" : undefined}
                >
                  <section.icon />
                  <span>{section.label}</span>
                </Link>
              </SidebarMenuButton>
            </SidebarMenuItem>
          );
        })}
      </SidebarMenu>
    </SidebarGroup>
  );
}
