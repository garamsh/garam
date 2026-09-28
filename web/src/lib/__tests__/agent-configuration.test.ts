import { describe, expect, it } from "vitest";

import { configurationStatus } from "@/lib/agent-configuration";
import type { components } from "@/types/api";

type Agent = components["schemas"]["Agent"];
type Report = NonNullable<Agent["configuration"]>;

const reportedAt = "2026-09-28T01:02:03Z";

/** An agent held at epoch 2, carrying the report given, if any. */
function held(configuration?: Partial<Report>): Agent {
  return {
    grn: "grn:acme:default:agent:9f2ac1b40d8e7a35",
    orgGrn: "grn:root:default:org:acme",
    provisioningState: "ready",
    runtimeState: "connected",
    createdAt: reportedAt,
    updatedAt: reportedAt,
    healthReportedAt: reportedAt,
    assignment: {
      operatorGrn: "grn:acme:default:operator:field-one",
      epoch: 2,
      since: reportedAt,
    },
    configuration: configuration && {
      epoch: 2,
      revision: 3,
      outcome: "applied",
      ignoredKeys: [],
      reportedAt,
      ...configuration,
    },
  };
}

describe("configurationStatus", () => {
  it("reads a ready agent that never acknowledged as unknown, not applied", () => {
    expect(configurationStatus(3, held())).toEqual({
      kind: "unknown",
      reason: "unreported",
    });
    // The control: the same agent acknowledging revision 3 is applied.
    expect(configurationStatus(3, held({}))).toEqual({
      kind: "applied",
      reportedAt,
    });
  });

  it("reads an agent no operator has reported as unknown", () => {
    expect(configurationStatus(1, undefined)).toEqual({
      kind: "unknown",
      reason: "unreported",
    });
  });

  it("reads a refused list of reports as unchecked", () => {
    expect(configurationStatus(1, "unchecked")).toEqual({
      kind: "unchecked",
    });
  });

  it("reads a replaced operator's report as unknown beside the current one's", () => {
    expect(configurationStatus(3, held({ epoch: 1 }))).toEqual({
      kind: "unknown",
      reason: "replaced",
    });
    expect(configurationStatus(3, held({ epoch: 2 })).kind).toBe("applied");
  });

  it("reads a report with no assignment beside it as unknown", () => {
    const agent = { ...held({}), assignment: undefined };
    expect(configurationStatus(3, agent)).toEqual({
      kind: "unknown",
      reason: "unassigned",
    });
  });

  it("reads an older acknowledged revision as pending", () => {
    expect(configurationStatus(3, held({ revision: 2 }))).toEqual({
      kind: "pending",
      acknowledged: 2,
      reportedAt,
    });
  });

  it("reads a revision saved after the settings were read as unknown", () => {
    expect(configurationStatus(3, held({ revision: 4 }))).toEqual({
      kind: "unknown",
      reason: "newer",
    });
  });

  it("reads an application that ignored keys as partial, never applied", () => {
    expect(
      configurationStatus(3, held({ ignoredKeys: ["model", "workspace"] })),
    ).toEqual({
      kind: "partial",
      ignoredKeys: ["model", "workspace"],
      reportedAt,
    });
    expect(configurationStatus(3, held({ ignoredKeys: [] })).kind).toBe(
      "applied",
    );
  });

  it("reads a rejection with its reason", () => {
    expect(
      configurationStatus(
        3,
        held({
          outcome: "rejected",
          ignoredKeys: undefined,
          reason: "image pull failed",
        }),
      ),
    ).toEqual({ kind: "rejected", reason: "image pull failed", reportedAt });
  });
});
