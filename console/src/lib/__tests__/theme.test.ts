import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  THEME_SCRIPT,
  THEME_STORAGE_KEY,
  applyTheme,
  parseThemePreference,
} from "@/lib/theme";
import { stubLocalStorage } from "@/testing/storage";

function systemPrefersDark(dark: boolean) {
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: dark && query === "(prefers-color-scheme: dark)",
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  }));
}

beforeEach(() => {
  stubLocalStorage();
});

afterEach(() => {
  window.localStorage.clear();
  document.documentElement.className = "";
  document.documentElement.style.colorScheme = "";
  systemPrefersDark(false);
});

describe("parseThemePreference", () => {
  it("keeps light and dark and reads anything else as system", () => {
    expect(parseThemePreference("light")).toBe("light");
    expect(parseThemePreference("dark")).toBe("dark");
    expect(parseThemePreference("sepia")).toBe("system");
    expect(parseThemePreference(null)).toBe("system");
  });
});

describe("applyTheme", () => {
  it("follows the system when nothing is stored", () => {
    systemPrefersDark(true);
    applyTheme();
    expect(document.documentElement).toHaveClass("dark");
    expect(document.documentElement.style.colorScheme).toBe("dark");
  });

  it("lets a stored light preference win over a dark system", () => {
    systemPrefersDark(true);
    window.localStorage.setItem(THEME_STORAGE_KEY, "light");
    applyTheme();
    expect(document.documentElement).not.toHaveClass("dark");
    expect(document.documentElement.style.colorScheme).toBe("light");
  });

  it("applies a preference it is handed over the stored one", () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, "light");
    applyTheme("dark");
    expect(document.documentElement).toHaveClass("dark");
  });
});

describe("THEME_SCRIPT", () => {
  it("applies the stored preference when run on its own", () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, "dark");
    // The script runs in <head> with nothing from this module in scope.
    new Function(THEME_SCRIPT)();
    expect(document.documentElement).toHaveClass("dark");
  });
});
