import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useIsMobile } from "../use-mobile";

// stubViewport returns a setter that moves the viewport across the
// breakpoint and notifies subscribers, as a real `MediaQueryList` does.
function stubViewport(initial: boolean): (matches: boolean) => void {
  const listeners = new Set<() => void>();
  let matches = initial;

  vi.stubGlobal("matchMedia", (media: string) => ({
    media,
    get matches() {
      return matches;
    },
    addEventListener: (_type: string, listener: () => void) => {
      listeners.add(listener);
    },
    removeEventListener: (_type: string, listener: () => void) => {
      listeners.delete(listener);
    },
  }));

  return (next: boolean) => {
    matches = next;
    for (const listener of listeners) listener();
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("useIsMobile", () => {
  it("reports the viewport it is first rendered into", () => {
    stubViewport(true);

    const { result } = renderHook(() => useIsMobile());

    expect(result.current).toBe(true);
  });

  it("follows the viewport across the breakpoint", () => {
    const resize = stubViewport(false);

    const { result } = renderHook(() => useIsMobile());
    act(() => resize(true));

    expect(result.current).toBe(true);
  });
});
