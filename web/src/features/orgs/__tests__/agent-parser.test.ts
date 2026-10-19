import { describe, expect, it } from "vitest";

import { AgentDefinitionParser, AgentParser } from "@/features/orgs/api/orgs";

const agent = {
  grn: "grn:acme:default:agent:9f2ac1b40d8e7a35",
  orgGrn: "grn:root:default:org:acme",
  provisioningState: "pending",
  runtimeState: "unknown",
  createdAt: "2026-01-02T03:04:05Z",
  updatedAt: "2026-01-02T03:04:05Z",
};

describe("AgentParser", () => {
  it("loads an assignment by its assignee alone, and reads no legacy operatorGrn", () => {
    const held = {
      ...agent,
      assignment: {
        assigneeGrn: "grn:acme:default:supervisor:0a1b2c3d4e5f6071",
        epoch: 1,
        since: "2026-01-02T03:04:05Z",
      },
    };
    // An operator-held agent may still name its operator on the legacy
    // field; the console reads the assignee instead, so the backend can
    // drop it (#1592).
    const operated = {
      ...agent,
      assignment: {
        assigneeGrn: "grn:acme:default:operator:field-one",
        operatorGrn: "grn:acme:default:operator:field-one",
        epoch: 2,
        since: "2026-01-02T03:04:05Z",
      },
    };
    expect(AgentParser.parse(held).assignment?.assigneeGrn).toBe(
      "grn:acme:default:supervisor:0a1b2c3d4e5f6071",
    );
    expect(AgentParser.parse(operated).assignment).toEqual({
      assigneeGrn: "grn:acme:default:operator:field-one",
      epoch: 2,
      since: "2026-01-02T03:04:05Z",
    });
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

const definition = {
  agentGrn: "grn:acme:default:agent:9f2ac1b40d8e7a35",
  orgGrn: "grn:root:default:org:acme",
  name: "Scout",
  operatorGrn: "grn:acme:default:operator:field-one",
  values: { model: "haiku" },
  definedBy: "grn:root:default:user:alice",
  createdAt: "2026-01-02T03:04:05Z",
};

describe("AgentDefinitionParser", () => {
  it("loads the stage of an open cutover, and none where none is open", () => {
    expect(
      AgentDefinitionParser.parse({
        ...definition,
        cutover: { stage: "frozen" },
      }).cutover,
    ).toEqual({ stage: "frozen" });
    expect(
      AgentDefinitionParser.parse({
        ...definition,
        cutover: { stage: "switched" },
      }).cutover,
    ).toEqual({ stage: "switched" });
    expect(
      AgentDefinitionParser.parse({ ...definition, cutover: null }).cutover,
    ).toBeNull();
  });

  it("refuses a stage the browser read never carries open", () => {
    // garam reads a rolled-back attempt as none open (ADR-0086), so a
    // definition naming it is a read this console does not understand.
    // The control: the same definition loads with an open stage, so the
    // refusal is the stage and not the shape around it.
    expect(() =>
      AgentDefinitionParser.parse({
        ...definition,
        cutover: { stage: "switched" },
      }),
    ).not.toThrow();
    expect(() =>
      AgentDefinitionParser.parse({
        ...definition,
        cutover: { stage: "rolled_back" },
      }),
    ).toThrow();
  });
});
