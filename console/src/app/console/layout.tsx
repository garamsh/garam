import { Suspense } from "react";

import { Separator } from "@/components/ui/separator";
import { SidebarInset, SidebarTrigger } from "@/components/ui/sidebar";
import { Skeleton } from "@/components/ui/skeleton";
import { TooltipProvider } from "@/components/ui/tooltip";

import { CommandPalette } from "./_components/command-palette";
import { ConsoleBreadcrumb } from "./_components/console-breadcrumb";
import { ConsoleCommand } from "./_components/console-command";
import {
  ConsoleSidebar,
  ConsoleSidebarFallback,
} from "./_components/console-sidebar";
import { ConsoleSidebarProvider } from "./_components/console-sidebar-provider";
import { ItemCrumbProvider } from "./_components/item-crumb";

/**
 * The authenticated shell (ADR-0011): a sidebar, a 48px top bar with
 * the breadcrumb and the command palette, and the page. Every read it
 * needs sits behind `<Suspense>` — the uncached `/me` and `/orgs`
 * reads, and the sidebar's stored state — because awaiting one at this
 * level would block the whole segment and no route's `loading.tsx`
 * could stream (stack-nextjs.md §App Router file conventions).
 */
export default function ConsoleLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <TooltipProvider delayDuration={300}>
      <Suspense fallback={null}>
        <ConsoleSidebarProvider>
          <Suspense fallback={<ConsoleSidebarFallback />}>
            <ConsoleSidebar />
          </Suspense>
          <ItemCrumbProvider>
            <SidebarInset className="min-w-0">
              <header className="sticky top-0 z-10 flex h-topbar shrink-0 items-center gap-2 border-b bg-background/95 px-3 backdrop-blur supports-[backdrop-filter]:bg-background/80 md:px-4">
                <SidebarTrigger className="-ml-1" />
                <Separator
                  orientation="vertical"
                  className="mr-1 data-vertical:h-4 data-vertical:self-auto"
                />
                <div className="flex min-w-0 flex-1 items-center">
                  <Suspense fallback={<Skeleton className="h-4 w-40" />}>
                    <ConsoleBreadcrumb />
                  </Suspense>
                </div>
                <Suspense fallback={<CommandPalette orgs={[]} />}>
                  <ConsoleCommand />
                </Suspense>
              </header>
              <div className="mx-auto flex w-full max-w-content flex-1 flex-col gap-6 px-4 py-6 md:px-8 md:py-8">
                {children}
              </div>
            </SidebarInset>
          </ItemCrumbProvider>
        </ConsoleSidebarProvider>
      </Suspense>
    </TooltipProvider>
  );
}
