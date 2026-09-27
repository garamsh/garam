import {
  parseGrn,
  slugFromGrn,
  type Agent,
  type GroupMember,
  type Member,
} from "@/features/orgs";

/** What a team member is, read from its GRN's service type. */
export type MemberKind = "person" | "agent" | "unknown";

/**
 * One membership of a team, as the page shows it. `name` is what this
 * organization calls the member, null where it has no name for it;
 * `resolved` says whether the organization still holds the identity
 * the membership names. Neither is inferred from the other: a person
 * whose user row is gone is resolved and nameless.
 */
export type TeamMember = {
  grn: string;
  identifier: string;
  kind: MemberKind;
  name: string | null;
  resolved: boolean;
};

/** A person or an agent the team does not hold yet. */
export type Candidate = {
  grn: string;
  kind: "person" | "agent";
  label: string;
};

/** What a member GRN names, by its service type. */
function kindOf(grn: string): MemberKind {
  switch (parseGrn(grn)?.serviceType) {
    case "user":
      return "person";
    case "agent":
      return "agent";
    default:
      return "unknown";
  }
}

/**
 * Reads each membership against the organization's people and agents.
 * A GRN neither list holds stays in the result, marked unresolved, so a
 * membership that names nobody any more is still visible and removable.
 */
export function resolveTeamMembers(
  members: readonly GroupMember[],
  people: readonly Member[],
  agents: readonly Agent[],
): TeamMember[] {
  return members.map(({ memberGrn }) => {
    const identifier = slugFromGrn(memberGrn);
    const kind = kindOf(memberGrn);
    switch (kind) {
      case "person": {
        const person = people.find((p) => p.userGrn === memberGrn);
        return {
          grn: memberGrn,
          identifier,
          kind,
          name: person?.nickname ?? null,
          resolved: person !== undefined,
        };
      }
      case "agent": {
        // An agent carries no name of its own (#1125), so its
        // identifier is the name the console has for it.
        const resolved = agents.some((a) => a.grn === memberGrn);
        return { grn: memberGrn, identifier, kind, name: identifier, resolved };
      }
      case "unknown":
        return {
          grn: memberGrn,
          identifier,
          kind,
          name: null,
          resolved: false,
        };
    }
  });
}

/**
 * The people and agents of the organization the team does not hold,
 * people first. A person without a name on record is offered under
 * their identifier, said to be one.
 */
export function teamCandidates(
  members: readonly GroupMember[],
  people: readonly Member[],
  agents: readonly Agent[],
): Candidate[] {
  const held = new Set(members.map((member) => member.memberGrn));
  return [
    ...people
      .filter((person) => !held.has(person.userGrn))
      .map((person) => ({
        grn: person.userGrn,
        kind: "person" as const,
        label:
          person.nickname ??
          `No name on record (${slugFromGrn(person.userGrn)})`,
      })),
    ...agents
      .filter((agent) => !held.has(agent.grn))
      .map((agent) => ({
        grn: agent.grn,
        kind: "agent" as const,
        label: slugFromGrn(agent.grn),
      })),
  ];
}

/** How a member is named in a sentence: its name, or what it is. */
export function memberLabel(member: TeamMember): string {
  if (member.name !== null) return member.name;
  switch (member.kind) {
    case "person":
      return `the person ${member.identifier}`;
    case "agent":
      return `the agent ${member.identifier}`;
    case "unknown":
      return `the unknown member ${member.identifier}`;
  }
}

/** The word a member's type is shown as. */
export const KIND_LABEL: Record<MemberKind, string> = {
  person: "Person",
  agent: "Agent",
  unknown: "Unknown",
};
