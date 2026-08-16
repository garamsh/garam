import { Suspense } from "react";

import { Separator } from "@/components/ui/separator";
import { SidebarInset, SidebarTrigger } from "@/components/ui/sidebar";
import { Skeleton } from "@/components/ui/skeleton";
import { TooltipProvider } from "@/components/ui/tooltip";

import { ConsoleBreadcrumb } from "./_components/console-breadcrumb";
import {
  ConsoleSidebar,
  ConsoleSidebarFallback,
} from "./_components/console-sidebar";
import { ConsoleSidebarProvider } from "./_components/console-sidebar-provider";

/**
 * The authenticated shell (ADR-0011). Every read it needs sits behind
 * `<Suspense>` — the uncached `/me` and `/orgs` reads, and the sidebar's
 * stored state — because awaiting one at this level would block the whole
 * segment and no route's `loading.tsx` could stream (stack-nextjs.md §2).
 */
export default function ConsoleLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <TooltipProvider>
      <Suspense fallback={null}>
        <ConsoleSidebarProvider>
          <Suspense fallback={<ConsoleSidebarFallback />}>
            <ConsoleSidebar />
          </Suspense>
          <SidebarInset>
            <header className="flex h-14 shrink-0 items-center border-b">
              <div className="mx-auto flex w-full max-w-shell items-center gap-2 px-4">
                <SidebarTrigger className="-ml-1" />
                <Separator
                  orientation="vertical"
                  className="mr-1 data-vertical:h-4 data-vertical:self-auto"
                />
                <Suspense fallback={<Skeleton className="h-4 w-40" />}>
                  <ConsoleBreadcrumb />
                </Suspense>
              </div>
            </header>
            <div className="mx-auto flex w-full max-w-shell flex-1 flex-col gap-6 px-4 py-8">
              {children}
            </div>
          </SidebarInset>
        </ConsoleSidebarProvider>
      </Suspense>
    </TooltipProvider>
  );
}
