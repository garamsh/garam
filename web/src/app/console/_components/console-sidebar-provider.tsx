import { cookies } from "next/headers";
import type { ReactNode } from "react";

import { SidebarProvider } from "@/components/ui/sidebar";

// `SidebarProvider` writes this cookie on every toggle without exporting
// its name.
const SIDEBAR_COOKIE = "sidebar_state";

/**
 * ConsoleSidebarProvider seeds the sidebar from the cookie the primitive
 * writes, so a collapsed sidebar is served collapsed after a reload rather
 * than corrected once the page is interactive. The read sits behind the
 * layout's `<Suspense>` boundary: awaited at layout level it would stop
 * every route's `loading.tsx` streaming (stack-nextjs.md §App Router
 * file conventions, ADR-0011).
 */
export async function ConsoleSidebarProvider({
  children,
}: {
  children: ReactNode;
}) {
  const stored = (await cookies()).get(SIDEBAR_COOKIE)?.value;

  return (
    <SidebarProvider
      defaultOpen={stored !== "false"}
      // 240px open and 56px collapsed, over the primitive's defaults.
      style={
        {
          "--sidebar-width": "15rem",
          "--sidebar-width-icon": "3.5rem",
        } as React.CSSProperties
      }
    >
      {children}
    </SidebarProvider>
  );
}
