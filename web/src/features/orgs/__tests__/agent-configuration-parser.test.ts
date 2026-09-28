import { describe, expect, it } from "vitest";

import { AgentParser } from "@/features/orgs/api/orgs";
import { reportedAgent } from "@/testing/mocks/handlers";

const reportedAt = "2026-09-28T01:02:03Z";

/** The reported agent carrying `configuration` as garam sent it. */
function withReport(configuration: unknown): unknown {
  return { ...reportedAgent, configuration };
}

describe("AgentParser configuration", () => {
  it("reads an agent with no report as one with none, not as applied", () => {
    const parsed = AgentParser.safeParse(reportedAgent);
    expect(parsed.success).toBe(true);
    expect(parsed.data?.configuration).toBeUndefined();
  });

  it.each<[string, Record<string, unknown>, Record<string, unknown>]>([
    [
      "an application naming no ignored keys",
      { outcome: "applied" },
      { outcome: "applied", ignoredKeys: [] },
    ],
    [
      "a rejection carrying no reason",
      { outcome: "rejected" },
      { outcome: "rejected", reason: "image pull failed" },
    ],
    [
      "an outcome garam does not name",
      { outcome: "partial", ignoredKeys: [] },
      { outcome: "applied", ignoredKeys: [] },
    ],
  ])(
    "refuses %s beside the report that carries it",
    (_case, refused, accepted) => {
      const base = { epoch: 1, revision: 1, reportedAt };
      expect(
        AgentParser.safeParse(withReport({ ...base, ...refused })).success,
      ).toBe(false);
      expect(
        AgentParser.safeParse(withReport({ ...base, ...accepted })).success,
      ).toBe(true);
    },
  );
});
