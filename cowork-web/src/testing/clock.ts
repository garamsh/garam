import { vi } from "vitest";

let now = Date.parse("2026-01-01T00:00:00Z");

/**
 * Moves the clock an hour on, so a page's last ask of the session
 * (`lib/socket-close.ts`) is out of date for the next test.
 */
export function startFreshPage() {
  now += 60 * 60 * 1000;
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(now);
}

/** Moves the clock `ms` on within one test. */
export function advance(ms: number) {
  now += ms;
  vi.setSystemTime(now);
}
