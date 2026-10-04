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
  it("loads an agent a standalone host's supervisor holds", () => {
    const held = {
      ...agent,
      assignment: {
        assigneeGrn: "grn:acme:default:supervisor:0a1b2c3d4e5f6071",
        epoch: 1,
        since: "2026-01-02T03:04:05Z",
      },
    };
    // The control: an operator-held agent, which also names its
    // operator on the legacy field, loads through the same parser, so
    // the refusal below is the missing assignee and not the shape.
    const operated = {
      ...agent,
      assignment: {
        assigneeGrn: "grn:acme:default:operator:field-one",
        operatorGrn: "grn:acme:default:operator:field-one",
        epoch: 2,
        since: "2026-01-02T03:04:05Z",
      },
    };
    expect(AgentParser.parse(held).assignment?.operatorGrn).toBeUndefined();
    expect(AgentParser.parse(operated).assignment?.operatorGrn).toBe(
      "grn:acme:default:operator:field-one",
    );
    expect(() =>
      AgentParser.parse({
        ...agent,
        assignment: { epoch: 1, since: "2026-01-02T03:04:05Z" },
      }),
    ).toThrow();
  });
});
