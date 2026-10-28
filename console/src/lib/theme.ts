/** The colour scheme a visitor asked for; `system` follows the OS. */
export type ThemePreference = "system" | "light" | "dark";

export const THEME_PREFERENCES: readonly ThemePreference[] = [
  "system",
  "light",
  "dark",
];

/** Where the preference is kept: per browser, never sent to a server. */
export const THEME_STORAGE_KEY = "garam-theme";

/** Reads a stored value; anything unrecognised means `system`. */
export function parseThemePreference(value: unknown): ThemePreference {
  return value === "light" || value === "dark" ? value : "system";
}

/**
 * Puts a preference on `<html>` — the one given, or the stored one: the `dark` class the theme
 * tokens key on, and the `color-scheme` form controls and scrollbars
 * follow. Self-contained, because it is also serialized into
 * `THEME_SCRIPT` and runs there with nothing from this module in scope —
 * which is why the storage key is written out rather than imported.
 */
export function applyTheme(preference?: string | null): void {
  let stored = preference ?? null;
  if (preference === undefined) {
    try {
      stored = window.localStorage.getItem("garam-theme");
    } catch {
      // Storage can be refused; the system preference still applies.
    }
  }
  const dark =
    stored === "dark" ||
    (stored !== "light" &&
      window.matchMedia("(prefers-color-scheme: dark)").matches);
  const root = document.documentElement;
  root.classList.toggle("dark", dark);
  root.style.colorScheme = dark ? "dark" : "light";
}

/**
 * Runs in `<head>` before the body paints, so a stored dark theme is
 * never first served light; it then follows the OS while the page is
 * open. The root layout renders it.
 */
export const THEME_SCRIPT = `(function(){var apply=${applyTheme.toString()};var stored=function(){apply()};stored();window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change",stored);window.addEventListener("storage",stored);})();`;
