/**
 * Where an agent's saved settings stand against what its operator
 * reported (ADR-0076). garam learns of application only from the
 * operator holding the agent, and that report is the operator's claim:
 * nothing here says a setting took effect, only what was reported.
 */

import type { components } from "@/types/api";

type Agent = components["schemas"]["Agent"];

/**
 * The state a revision is in. `unchecked` is a refused read of the
 * reports. `unknown` is no report about this revision under the
 * current assignment: none ever, one from an operator since replaced,
 * or one about a revision saved after this page read the settings.
 */
export type ConfigurationStatus =
  | { kind: "unchecked" }
  | {
      kind: "unknown";
      reason: "unreported" | "unassigned" | "replaced" | "newer";
    }
  | { kind: "pending"; acknowledged: number; reportedAt: string }
  | { kind: "applied"; reportedAt: string }
  | { kind: "partial"; ignoredKeys: string[]; reportedAt: string }
  | { kind: "rejected"; reason: string; reportedAt: string };

/**
 * The status of the saved revision, read from the agent's report. An
 * agent the reports were refused for is `unchecked`; one no operator
 * reported, or a report about another assignment, is `unknown`, which
 * is never shown as applied.
 */
export function configurationStatus(
  savedRevision: number,
  agent: Agent | undefined | "unchecked",
): ConfigurationStatus {
  if (agent === "unchecked") return { kind: "unchecked" };
  const report = agent?.configuration;
  if (agent === undefined || report === undefined) {
    return { kind: "unknown", reason: "unreported" };
  }
  if (agent.assignment === undefined) {
    return { kind: "unknown", reason: "unassigned" };
  }
  if (report.epoch !== agent.assignment.epoch) {
    return { kind: "unknown", reason: "replaced" };
  }
  const { reportedAt } = report;
  // The settings and the reports are two reads, so a revision saved
  // between them can already be acknowledged.
  if (report.revision > savedRevision) {
    return { kind: "unknown", reason: "newer" };
  }
  if (report.revision < savedRevision) {
    return { kind: "pending", acknowledged: report.revision, reportedAt };
  }
  switch (report.outcome) {
    case "rejected":
      return { kind: "rejected", reason: report.reason, reportedAt };
    case "applied":
      return report.ignoredKeys.length > 0
        ? { kind: "partial", ignoredKeys: report.ignoredKeys, reportedAt }
        : { kind: "applied", reportedAt };
  }
}
