import { describe, expect, it } from "vitest";

import { AgentParser } from "@/features/orgs/api/orgs";

const agent = {
  grn: "grn:acme:default:agent:9f2ac1b40d8e7a35",
  orgGrn: "grn:root:default:org:acme",
  provisioningState: "pending",
  runtimeState: "unknown",
  createdAt: "2026-01-02T03:04:05Z",
  updatedAt: "2026-01-02T03:04:05Z",
};

describe("AgentParser", () => {
  it("loads an assignment by its assignee alone", () => {
    const held = {
      ...agent,
      assignment: {
        assigneeGrn: "grn:acme:default:supervisor:0a1b2c3d4e5f6071",
        epoch: 1,
        since: "2026-01-02T03:04:05Z",
      },
    };
    expect(AgentParser.parse(held).assignment?.assigneeGrn).toBe(
      "grn:acme:default:supervisor:0a1b2c3d4e5f6071",
    );
    // The control: the same shape without an assignee is refused, so
    // the loads above are the assignee and not a lax parser.
    expect(() =>
      AgentParser.parse({
        ...agent,
        assignment: { epoch: 1, since: "2026-01-02T03:04:05Z" },
      }),
    ).toThrow();
  });

  it("keeps the generation garam last activated, where it answers one", () => {
    expect(AgentParser.parse({ ...agent, generation: "7" }).generation).toBe(
      "7",
    );
    expect(AgentParser.parse(agent).generation).toBeUndefined();
  });
});
