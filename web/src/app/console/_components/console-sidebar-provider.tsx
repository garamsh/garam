"use client";

import { useState, useSyncExternalStore, type ReactNode } from "react";

import { SidebarProvider } from "@/components/ui/sidebar";

// `SidebarProvider` writes this cookie on every toggle without exporting
// its name.
const SIDEBAR_COOKIE = "sidebar_state";

// The stored value is read once, at hydration: every later write comes
// from a toggle this component already re-renders for.
function subscribe(): () => void {
  return () => {};
}

function readStoredOpen(): boolean {
  return !document.cookie.split("; ").includes(`${SIDEBAR_COOKIE}=false`);
}

/**
 * ConsoleSidebarProvider seeds the sidebar from the cookie the primitive
 * writes, so a collapsed sidebar is still collapsed after a reload. Server
 * rendering and hydration both read expanded, because reading the cookie in
 * the layout would await a dynamic API at layout level and stop every
 * route's `loading.tsx` streaming (stack-nextjs.md §2, ADR-0011).
 */
export function ConsoleSidebarProvider({ children }: { children: ReactNode }) {
  const storedOpen = useSyncExternalStore(
    subscribe,
    readStoredOpen,
    () => true,
  );
  const [toggledOpen, setToggledOpen] = useState<boolean | null>(null);

  return (
    <SidebarProvider
      open={toggledOpen ?? storedOpen}
      onOpenChange={setToggledOpen}
    >
      {children}
    </SidebarProvider>
  );
}
