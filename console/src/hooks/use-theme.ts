"use client";

import { useCallback, useSyncExternalStore } from "react";

import {
  THEME_STORAGE_KEY,
  applyTheme,
  parseThemePreference,
  type ThemePreference,
} from "@garam/design/lib/theme";

// `storage` fires in other tabs only; this one is dispatched here.
const CHANGE_EVENT = "garam-theme-change";

function subscribe(onChange: () => void): () => void {
  window.addEventListener(CHANGE_EVENT, onChange);
  window.addEventListener("storage", onChange);
  return () => {
    window.removeEventListener(CHANGE_EVENT, onChange);
    window.removeEventListener("storage", onChange);
  };
}

function readPreference(): ThemePreference {
  try {
    return parseThemePreference(window.localStorage.getItem(THEME_STORAGE_KEY));
  } catch {
    return "system";
  }
}

/**
 * The stored theme preference and the way to change it. The server has
 * no preference to render, so it renders `system` and the client
 * corrects the control after hydration; the page itself was already
 * painted in the right scheme by `THEME_SCRIPT`.
 */
export function useTheme(): {
  theme: ThemePreference;
  setTheme: (theme: ThemePreference) => void;
} {
  const theme = useSyncExternalStore(
    subscribe,
    readPreference,
    (): ThemePreference => "system",
  );
  const setTheme = useCallback((next: ThemePreference) => {
    try {
      if (next === "system") {
        window.localStorage.removeItem(THEME_STORAGE_KEY);
      } else {
        window.localStorage.setItem(THEME_STORAGE_KEY, next);
      }
    } catch {
      // A refused write still themes this page; it does not persist.
    }
    applyTheme(next);
    window.dispatchEvent(new Event(CHANGE_EVENT));
  }, []);
  return { theme, setTheme };
}
