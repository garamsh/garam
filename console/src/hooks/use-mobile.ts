import { useSyncExternalStore } from "react";

const MOBILE_BREAKPOINT = 768;
const MOBILE_QUERY = `(max-width: ${MOBILE_BREAKPOINT - 1}px)`;

function subscribe(onChange: () => void): () => void {
  const query = window.matchMedia(MOBILE_QUERY);
  query.addEventListener("change", onChange);
  return () => query.removeEventListener("change", onChange);
}

/**
 * useIsMobile reports whether the viewport sits below the mobile
 * breakpoint. Server rendering and hydration both read `false`, because
 * the viewport width is unknown until the client subscribes. This hook is
 * no longer shadcn output — `shadcn add` of a sidebar-dependent primitive
 * would restore the effect version the lint rejects.
 */
export function useIsMobile(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => window.matchMedia(MOBILE_QUERY).matches,
    () => false,
  );
}
