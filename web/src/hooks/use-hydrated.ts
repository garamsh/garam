import { useSyncExternalStore } from "react";

const subscribe = () => () => {};

/**
 * False in the server render and while hydrating, true once React runs
 * the component in the browser. A controlled field disabled until then
 * cannot take input React would not see.
 */
export function useHydrated(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => true,
    () => false,
  );
}
