import { describe, expect, it } from "vitest";

import { formatTime } from "@/lib/format-time";

const now = new Date("2026-10-14T09:00:00Z");

describe("formatTime", () => {
  it("phrases a recent instant relative to now", () => {
    expect(formatTime("2026-10-14T08:57:00Z", { now })?.relative).toBe(
      "3 minutes ago",
    );
    expect(formatTime("2026-10-14T06:00:00Z", { now })?.relative).toBe(
      "3 hours ago",
    );
    expect(formatTime("2026-10-12T09:00:00Z", { now })?.relative).toBe(
      "2 days ago",
    );
  });

  it("says now for an instant under a minute away", () => {
    expect(formatTime("2026-10-14T08:59:30Z", { now })?.relative).toBe("now");
  });

  it("phrases a future instant as ahead", () => {
    expect(formatTime("2026-10-14T11:00:00Z", { now })?.relative).toBe(
      "in 2 hours",
    );
  });

  it("gives the date rather than a phrase past thirty days", () => {
    expect(
      formatTime("2026-03-03T12:00:00Z", { now, timeZone: "UTC" })?.relative,
    ).toBe("Mar 3, 2026");
  });

  it("states the exact time with its zone", () => {
    expect(
      formatTime("2026-10-14T08:57:05Z", { now, timeZone: "Asia/Seoul" })
        ?.absolute,
    ).toBe("Oct 14, 2026, 17:57:05 GMT+9");
    expect(
      formatTime("2026-10-14T08:57:05Z", { now, timeZone: "UTC" })?.absolute,
    ).toBe("Oct 14, 2026, 08:57:05 UTC");
  });

  it("phrases it in the locale asked for", () => {
    expect(
      formatTime("2026-10-14T08:57:00Z", { now, locale: "ko" })?.relative,
    ).toBe("3분 전");
  });

  it("keeps the machine-readable instant", () => {
    expect(formatTime("2026-10-14T08:57:05+09:00", { now })?.iso).toBe(
      "2026-10-13T23:57:05.000Z",
    );
  });

  it("refuses a value that is not a time", () => {
    expect(formatTime("not a time", { now })).toBeNull();
  });
});
