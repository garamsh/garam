import {
  agentChoiceLabel,
  directoryAgents,
  lookUpAgent,
  parseGrn,
  slugFromGrn,
  type AgentDirectory,
  type GroupMember,
  type Member,
} from "@/features/orgs";

/** What a team member is, read from its GRN's service type. */
export type MemberKind = "person" | "agent" | "unknown";

/**
 * A list the page looks members up in, or the fact that garam refused
 * the caller that list. A refused list resolves nobody and names no
 * one missing.
 */
export type Catalog<T> =
  { status: "ok"; data: readonly T[] } | { status: "forbidden" };

/**
 * What looking a member up found: the identity, no such identity in
 * the organization, or nothing, because the list it would be in was
 * refused to the caller.
 */
export type Lookup = "found" | "missing" | "refused";

/**
 * One membership of a team, as the page shows it. `name` is what this
 * organization calls the member, null where it has no name for it.
 * Neither it nor `lookup` is inferred from the other: a person whose
 * user row is gone is found and nameless.
 */
export type TeamMember = {
  grn: string;
  identifier: string;
  kind: MemberKind;
  name: string | null;
  lookup: Lookup;
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
    const kind = kindOf(memberGrn);
    switch (kind) {
      case "person": {
        const { lookup, entry } = lookUp(people, memberGrn, (p) => p.userGrn);
        return {
          grn: memberGrn,
          identifier,
          kind,
          name: entry?.nickname ?? null,
          lookup,
        };
      }
      case "agent": {
        // An agent no definition names has only its identifier.
        const { lookup, agent } = lookUpAgent(agents, memberGrn);
        return {
          grn: memberGrn,
          identifier,
          kind,
          name: agent?.name ?? identifier,
          lookup,
        };
      }
      case "unknown":
        return {
          grn: memberGrn,
          identifier,
          kind,
          name: null,
          lookup: "missing",
        };
    }
  });
}

/**
 * The people and agents of the organization the team does not hold,
 * people first. A person without a name on record is offered under
 * their identifier, said to be one. An agent is offered by name with
 * its identifier, including one still awaiting an operator: its GRN is
 * minted with its definition, and a team holds GRNs (ADR-0075). A
 * refused list offers no one.
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
        label:
          person.nickname ??
          `No name on record (${slugFromGrn(person.userGrn)})`,
      })),
    ...directoryAgents(agents)
      .filter((agent) => !held.has(agent.grn))
      .map((agent): Candidate => ({
        grn: agent.grn,
        kind: "agent",
        label: agentChoiceLabel(agent),
      })),
  ];
}

/**
 * Whether a member is an agent a definition names. Such a name is not
 * unique (ADR-0075), so wherever it stands the identifier stands too.
 */
export function isNamedAgent(member: TeamMember): boolean {
  return (
    member.kind === "agent" &&
    member.name !== null &&
    member.name !== member.identifier
  );
}

/**
 * How a member is named in a sentence: its name, or what it is. A
 * named agent carries its identifier, so two agents of one name are
 * two different sentences.
 */
export function memberLabel(member: TeamMember): string {
  if (isNamedAgent(member)) return `${member.name} (${member.identifier})`;
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
