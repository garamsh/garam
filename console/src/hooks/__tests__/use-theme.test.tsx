import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { useTheme } from "@/hooks/use-theme";
import { THEME_STORAGE_KEY } from "@garam/design/lib/theme";
import { stubLocalStorage } from "@garam/design/testing/storage";

beforeEach(() => {
  stubLocalStorage();
});

afterEach(() => {
  window.localStorage.clear();
  document.documentElement.className = "";
});

describe("useTheme", () => {
  it("reads the stored preference", () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, "dark");
    const { result } = renderHook(() => useTheme());
    expect(result.current.theme).toBe("dark");
  });

  it("stores a choice, applies it, and reports it back", () => {
    const { result } = renderHook(() => useTheme());
    act(() => result.current.setTheme("dark"));

    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");
    expect(document.documentElement).toHaveClass("dark");
    expect(result.current.theme).toBe("dark");
  });

  it("forgets the stored choice when system is chosen", () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, "dark");
    const { result } = renderHook(() => useTheme());
    act(() => result.current.setTheme("system"));

    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBeNull();
    // The test setup's system prefers light.
    expect(document.documentElement).not.toHaveClass("dark");
    expect(result.current.theme).toBe("system");
  });
});
