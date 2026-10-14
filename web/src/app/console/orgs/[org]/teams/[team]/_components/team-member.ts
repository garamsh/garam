import {
  UNNAMED_AGENT,
  agentChoiceLabel,
  directoryAgents,
  kindFromGrn,
  lookUpAgent,
  slugFromGrn,
  type AgentDirectory,
  type Catalog,
  type GroupMember,
  type Lookup,
  type Member,
  type SubjectKind,
} from "@/features/orgs";

/**
 * What a team member is, read from its GRN's service type. A team
 * holds no team, so a member GRN naming one is of no known kind.
 */
export type MemberKind = Exclude<SubjectKind, "team"> | "unknown";

/**
 * One membership of a team, as the page shows it. `label` is what this
 * organization calls the member, null where it has no name for it.
 * Neither it nor `lookup` is inferred from the other: a person whose
 * user row is gone is found and nameless.
 */
export type TeamMember = {
  grn: string;
  identifier: string;
  kind: MemberKind;
  label: string | null;
  lookup: Lookup;
};

/** A person or an agent the team does not hold yet. */
export type Candidate = {
  grn: string;
  kind: "person" | "agent";
  label: string;
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
        };
      }
      case "agent": {
        // An agent no definition names has only its identifier.
        const { lookup, agent } = lookUpAgent(agents, memberGrn);
        return {
          grn: memberGrn,
          identifier,
          kind,
          label: agent?.name ?? identifier,
          lookup,
        };
      }
      case "unknown":
        return {
          grn: memberGrn,
          identifier,
          kind,
          label: null,
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
    member.label !== null &&
    member.label !== member.identifier
  );
}

/**
 * How a member is named in a sentence: its name, or what it is. An
 * agent carries its identifier beside the name, or beside "Unnamed
 * agent" where it has none, so two agents are two different sentences.
 */
export function memberLabel(member: TeamMember): string {
  if (isNamedAgent(member)) return `${member.label} (${member.identifier})`;
  if (member.kind === "agent") {
    return `${UNNAMED_AGENT} (${member.identifier})`;
  }
  if (member.label !== null) return member.label;
  switch (member.kind) {
    case "person":
      return `the person ${member.identifier}`;
    case "unknown":
      return `the unknown member ${member.identifier}`;
  }
}
