import { redirect } from "next/navigation";

import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarRail,
} from "@/components/ui/sidebar";
import { Skeleton } from "@/components/ui/skeleton";
import { getSession } from "@/features/auth";

import { ConsoleNav } from "./console-nav";
import { listConsoleOrgs } from "./console-orgs";
import { OrgSwitcher } from "./org-switcher";
import { UserMenu } from "./user-menu";

/**
 * The sidebar and everything it needs a request for. The session check
 * stays load-bearing here: `proxy.ts` only checks that a cookie exists,
 * so an expired or revoked session lands on the login page rather than
 * on an empty console.
 */
export async function ConsoleSidebar() {
  const [orgs, user] = await Promise.all([listConsoleOrgs(), getSession()]);
  if (!user) {
    redirect("/login");
  }

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <OrgSwitcher orgs={orgs} />
      </SidebarHeader>
      <SidebarContent>
        <ConsoleNav />
      </SidebarContent>
      <SidebarFooter>
        <UserMenu nickname={user.nickname} />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}

/** The sidebar's frame while its two reads are in flight. */
export function ConsoleSidebarFallback() {
  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <Skeleton className="h-12 w-full" />
      </SidebarHeader>
      <SidebarContent />
      <SidebarFooter>
        <Skeleton className="h-12 w-full" />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}
