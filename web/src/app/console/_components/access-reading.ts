import type { ExplainedStatement } from "@/lib/permission-explanation";

/**
 * How far from the caller a statement reaches them: the organization,
 * a project, a team, or the caller themselves. Access is read from the
 * root down (ADR-0113), so a reading lists the organization first.
 * `unread` is a policy the caller may not read, whose path garam does
 * not say, so it is placed nowhere and read last.
 */
export type Scope = "organization" | "project" | "team" | "you" | "unread";

const ORDER: Record<Scope, number> = {
  organization: 0,
  project: 1,
  team: 2,
  you: 3,
  unread: 4,
};

/** One line of a reading: where the grant sits and what it is. */
export type GrantLine = {
  scope: Scope;
  effect: ExplainedStatement["effect"];
  text: string;
};

function roleList(roles: readonly string[] | undefined): string {
  const list = roles ?? [];
  return list.length === 1 ? `role ${list[0]}` : `roles ${list.join(", ")}`;
}

/** Where one statement sits, and the sentence naming it. */
function line(statement: ExplainedStatement): GrantLine {
  const { effect } = statement;
  switch (statement.source) {
    case "org-role":
      return {
        scope: "organization",
        effect,
        text: `Your organization ${roleList(statement.roles)}`,
      };
    case "direct-role":
      return {
        scope: "you",
        effect,
        text: `The ${roleList(statement.roles)} stored on you`,
      };
    case "group-role":
      return {
        scope: "team",
        effect,
        text: `Team ${statement.group}'s ${roleList(statement.roles)}`,
      };
    case "project-role":
      return {
        scope: "project",
        effect,
        text: `Project ${statement.project}'s ${roleList(statement.roles)}`,
      };
    case "policy": {
      const named = statement.sid
        ? `Policy ${statement.policy}, statement ${statement.sid}`
        : `Policy ${statement.policy}`;
      switch (statement.attachment) {
        case "organization":
          return {
            scope: "organization",
            effect,
            text: `${named}, applied to the whole organization`,
          };
        case "group":
          return {
            scope: "team",
            effect,
            text: `${named}, attached to team ${statement.group}`,
          };
        default:
          return { scope: "you", effect, text: `${named}, attached to you` };
      }
    }
    case "unreadable-policy":
      return {
        scope: "unread",
        effect,
        text: "A policy you can't read",
      };
  }
}

/**
 * The statements that decided an action, as lines from the
 * organization down to the caller. Their order within one scope is
 * garam's.
 */
export function grantLines(
  statements: readonly ExplainedStatement[],
): GrantLine[] {
  return statements
    .map(line)
    .map((entry, index) => ({ entry, index }))
    .sort(
      (a, b) =>
        ORDER[a.entry.scope] - ORDER[b.entry.scope] || a.index - b.index,
    )
    .map(({ entry }) => entry);
}
