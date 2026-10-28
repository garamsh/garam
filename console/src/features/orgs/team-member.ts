import { slugFromGrn } from "@/lib/grn";

import {
  agentDisplayName,
  directoryAgents,
  lookUpAgent,
  type AgentDirectory,
} from "./agent-directory";
import type { GroupMember, Member } from "./api/orgs";
import {
  kindFromGrn,
  type Catalog,
  type Lookup,
  type SubjectKind,
} from "./subject";

/**
 * What a team member is, read from its GRN's service type. A team
 * holds no team, so a member GRN naming one is of no known kind.
 */
export type MemberKind = Exclude<SubjectKind, "team"> | "unknown";

/**
 * One membership of a team, as the page shows it. `label` is what this
 * organization calls the member, null where it has no name for it.
 * Neither it nor `lookup` is inferred from the other: a person whose
 * user row is gone is found and nameless. `nameRead` is false where the
 * list a name would come from was refused, so a null `label` there is
 * a name not shown rather than none.
 */
export type TeamMember = {
  grn: string;
  identifier: string;
  kind: MemberKind;
  label: string | null;
  lookup: Lookup;
  nameRead: boolean;
};

/**
 * A person or an agent the team does not hold yet: its name, or what
 * stands in for one, and the identifier that tells two of one name
 * apart, which the choice shows in mono beside the name.
 */
export type Candidate = {
  grn: string;
  kind: "person" | "agent";
  name: string;
  identifier: string;
  /** Where an agent stands, when no operator has reported it yet. */
  note?: string;
};

/** Looks `grn` up in `catalog` by the key `keyOf` reads. */
function lookUp<T>(
  catalog: Catalog<T>,
  grn: string,
  keyOf: (entry: T) => string,
): { lookup: Lookup; entry?: T } {
  if (catalog.status === "forbidden") return { lookup: "refused" };
  const entry = catalog.data.find((candidate) => keyOf(candidate) === grn);
  return entry === undefined
    ? { lookup: "missing" }
    : { lookup: "found", entry };
}

/**
 * Reads each membership against the organization's people and agents.
 * A GRN no list holds stays in the result, marked missing, so a
 * membership that names nobody any more is still visible and
 * removable; one whose list was refused is marked refused instead. An
 * agent is known by what operators reported and by what the
 * organization defined, so one awaiting an operator is found by name.
 */
export function resolveTeamMembers(
  members: readonly GroupMember[],
  people: Catalog<Member>,
  agents: AgentDirectory,
): TeamMember[] {
  return members.map(({ memberGrn }) => {
    const identifier = slugFromGrn(memberGrn);
    const subjectKind = kindFromGrn(memberGrn);
    const kind: MemberKind = subjectKind === "team" ? "unknown" : subjectKind;
    switch (kind) {
      case "person": {
        const { lookup, entry } = lookUp(people, memberGrn, (p) => p.userGrn);
        return {
          grn: memberGrn,
          identifier,
          kind,
          label: entry?.nickname ?? null,
          lookup,
          nameRead: people.status !== "forbidden",
        };
      }
      case "agent": {
        const { lookup, agent } = lookUpAgent(agents, memberGrn);
        return {
          grn: memberGrn,
          identifier,
          kind,
          label: agent?.name ?? null,
          lookup,
          nameRead: true,
        };
      }
      case "unknown":
        return {
          grn: memberGrn,
          identifier,
          kind,
          label: null,
          lookup: "missing",
          nameRead: true,
        };
    }
  });
}

/**
 * The people and agents of the organization the team does not hold,
 * people first, each with its identifier. A person without a name on
 * record is offered as having none, and an agent the organization calls
 * nothing by its identifier (ADR-0119). A refused list offers no one.
 */
export function teamCandidates(
  members: readonly GroupMember[],
  people: Catalog<Member>,
  agents: AgentDirectory,
): Candidate[] {
  const held = new Set(members.map((member) => member.memberGrn));
  return [
    ...(people.status === "ok" ? people.data : [])
      .filter((person) => !held.has(person.userGrn))
      .map((person): Candidate => ({
        grn: person.userGrn,
        kind: "person",
        name: person.nickname ?? "No name on record",
        identifier: slugFromGrn(person.userGrn),
      })),
    ...directoryAgents(agents)
      .filter((agent) => !held.has(agent.grn))
      .map((agent): Candidate => ({
        grn: agent.grn,
        kind: "agent",
        name: agentDisplayName(agent),
        identifier: agent.identifier,
      })),
  ];
}

/**
 * What stands in a name's place for a member without one: none on
 * record, one the organization no longer holds, one whose list was not
 * read, and an identity of neither type. A person's identifier never
 * stands there; an agent the organization calls nothing is shown by
 * its identifier (ADR-0119).
 */
export function memberName(member: TeamMember): string {
  if (member.label !== null) return member.label;
  if (member.kind === "unknown") return "Unknown identity";
  if (member.lookup === "refused" || !member.nameRead) {
    return "Name not available";
  }
  if (member.lookup === "missing") {
    return member.kind === "agent" ? "Unknown agent" : "Unknown person";
  }
  return member.kind === "agent" ? member.identifier : "No name on record";
}

/**
 * How a member is named in a sentence: its name, or what it is. No
 * identifier stands in a sentence; the row beside it offers that.
 */
export function memberLabel(member: TeamMember): string {
  if (member.label !== null) return member.label;
  const noun = member.kind === "agent" ? "agent" : "person";
  if (member.kind === "unknown") return "an unknown member";
  if (member.lookup === "refused" || !member.nameRead) {
    return `${member.kind === "agent" ? "an" : "a"} ${noun} whose name is not available`;
  }
  if (member.lookup === "missing") {
    return `${member.kind === "agent" ? "an" : "a"} ${noun} no longer in this organization`;
  }
  return member.kind === "agent"
    ? "an unnamed agent"
    : "a person with no name on record";
}
