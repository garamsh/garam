import type { ExplainedGround } from "@/lib/permission-explanation";

/**
 * How far from the caller a ground reaches them: the organization, a
 * project or a team. Access is read from the root down (ADR-0113), so a
 * reading lists the organization first. `policy` is a guardrail, a
 * permit, a system policy or an error, whose path the policy service
 * does not state, so it is placed nowhere and read last.
 */
export type Scope = "organization" | "project" | "team" | "policy";

const ORDER: Record<Scope, number> = {
  organization: 0,
  project: 1,
  team: 2,
  policy: 3,
};

/** One line of a reading: where the ground sits and what it is. */
export type GrantLine = {
  scope: Scope;
  text: string;
  /** The Cedar text of a guardrail or permit the caller may read. */
  detail?: string;
};

/** A policy by the name the caller may read, else by its identifier. */
function policyName(ground: ExplainedGround): string {
  return ground.name ?? ground.policy ?? "of another organization";
}

/** Where one ground sits, and the sentence naming it. */
function line(ground: ExplainedGround): GrantLine {
  switch (ground.kind) {
    case "role-link": {
      const role = `role ${ground.role}`;
      switch (ground.path) {
        case "team":
          return { scope: "team", text: `Team ${ground.team}'s ${role}` };
        case "project":
          return {
            scope: "project",
            text: `Project ${ground.project}'s ${role}`,
          };
        case "organization":
          return { scope: "organization", text: `Your organization ${role}` };
        default:
          return { scope: "policy", text: `A ${role} link` };
      }
    }
    case "guardrail":
      return {
        scope: "policy",
        text: `Guardrail ${policyName(ground)}`,
        detail: ground.text,
      };
    case "permit":
      return {
        scope: "policy",
        text: `Permit ${policyName(ground)}`,
        detail: ground.text,
      };
    case "system-policy":
      return { scope: "policy", text: `System policy ${ground.policy}` };
    case "error":
      return {
        scope: "policy",
        text: ground.policy
          ? `Policy ${ground.policy} failed to evaluate: ${ground.error}`
          : `The question was refused: ${ground.error}`,
      };
  }
}

/**
 * The grounds the policy service named for an action, as lines from
 * the organization down. Their order within one scope is the order
 * given.
 */
export function grantLines(grounds: readonly ExplainedGround[]): GrantLine[] {
  return grounds
    .map(line)
    .map((entry, index) => ({ entry, index }))
    .sort(
      (a, b) =>
        ORDER[a.entry.scope] - ORDER[b.entry.scope] || a.index - b.index,
    )
    .map(({ entry }) => entry);
}
