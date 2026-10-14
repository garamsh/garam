import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarRail,
} from "@/components/ui/sidebar";
import { Skeleton } from "@/components/ui/skeleton";
import { getSession } from "@/features/auth";
import { redirectToSignIn } from "@/lib/sign-in-redirect";

import { ConsoleNav } from "./console-nav";
import { listConsoleOrgs } from "./console-orgs";
import { OrgSwitcher } from "./org-switcher";
import { UserMenu } from "./user-menu";

/**
 * Centres the 32px controls in the 56px rail the collapsed sidebar
 * leaves: the primitive's 8px inset is sized for its own 48px rail.
 */
const RAIL_INSET =
  "group-data-[collapsible=icon]:**:data-[slot=sidebar-group]:px-3 group-data-[collapsible=icon]:**:data-[slot=sidebar-header]:px-3 group-data-[collapsible=icon]:**:data-[slot=sidebar-footer]:px-3";

/**
 * The sidebar and everything it needs a request for. The session check
 * stays load-bearing here: `proxy.ts` only checks that a cookie exists,
 * so an expired or revoked session lands on the login page rather than
 * on an empty console.
 *
 * A read the API never answered drops the control it fed and keeps the
 * rest. The layout mounts this behind a `<Suspense>` with no boundary,
 * so throwing here takes the whole shell down, and a shell that renders
 * is what leaves the page free to report the outage.
 */
export async function ConsoleSidebar() {
  const [orgs, session] = await Promise.all([listConsoleOrgs(), getSession()]);
  if (session.status === "signed-out") {
    await redirectToSignIn();
  }

  return (
    <Sidebar collapsible="icon" className={RAIL_INSET}>
      {orgs.status === "ok" && (
        <SidebarHeader>
          <OrgSwitcher orgs={orgs.orgs} />
        </SidebarHeader>
      )}
      <SidebarContent>
        <ConsoleNav />
      </SidebarContent>
      {session.status === "signed-in" && (
        <SidebarFooter>
          <UserMenu nickname={session.user.nickname} />
        </SidebarFooter>
      )}
      <SidebarRail />
    </Sidebar>
  );
}

/** The sidebar's frame while its two reads are in flight. */
export function ConsoleSidebarFallback() {
  return (
    <Sidebar collapsible="icon" className={RAIL_INSET}>
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
