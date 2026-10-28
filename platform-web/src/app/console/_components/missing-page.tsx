"use client";

import { usePathname } from "next/navigation";
import { useEffect, useSyncExternalStore } from "react";

// A not-found page renders below the shell, and the breadcrumb above it
// has to say so. As with the org role, a store rather than a context
// carries it, so no provider above the page re-renders on the report.
let missing: string | null = null;
const listeners = new Set<() => void>();

function report(next: string | null) {
  missing = next;
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** Marks the page at the current pathname as not found; renders nothing. */
export function MarkMissing() {
  const pathname = usePathname();
  useEffect(() => {
    report(pathname);
    return () => report(null);
  }, [pathname]);
  return null;
}

/** Whether the page at `pathname` rendered as not found. */
export function useMissing(pathname: string): boolean {
  const current = useSyncExternalStore(
    subscribe,
    () => missing,
    () => null,
  );
  return current === pathname;
}
