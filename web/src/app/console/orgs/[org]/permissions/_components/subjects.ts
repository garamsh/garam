import {
  parseGrn,
  slugFromGrn,
  type Agent,
  type Group,
  type GroupMember,
  type Member,
  type Policy,
  type PolicyAttachment,
} from "@/features/orgs";

import type { ChoiceGroup } from "./attach-policy-form";

/** What a policy can be attached to, as the console names it. */
export type SubjectKind = "team" | "person" | "agent";

/**
 * A list the page reads subjects from, or the fact that garam refused
 * the caller that list. A refused list names no one and proves no one
 * absent.
 */
export type Catalog<T> =
  { status: "ok"; data: readonly T[] } | { status: "forbidden" };

/** The organization's teams, people and agents, as the page read them. */
export type Catalogs = {
  teams: Catalog<Group>;
  people: Catalog<Member>;
  agents: Catalog<Agent>;
};

/** A team, person or agent of the organization, by name. */
export type Subject = {
  grn: string;
  kind: SubjectKind;
  identifier: string;
  label: string;
};

/**
 * One attachment's subject as the page shows it. `label` is what the
 * organization calls it, null where it has no name for it; `lookup`
 * says whether the list it would be in held it, did not, or was
 * refused.
 */
export type AttachedSubject = {
  grn: string;
  kind: SubjectKind | "unknown";
  identifier: string;
  label: string | null;
  lookup: "found" | "missing" | "refused";
};

/** The word a subject's type is shown as. */
export const KIND_LABEL: Record<SubjectKind | "unknown", string> = {
  team: "Team",
  person: "Person",
  agent: "Agent",
  unknown: "Unknown",
};

/** The plural a refused list is named by. */
export const KIND_PLURAL: Record<SubjectKind, string> = {
  team: "teams",
  person: "people",
  agent: "agents",
};

/** The heading a kind's options are grouped under. */
const KIND_HEADING: Record<SubjectKind, string> = {
  team: "Teams",
  person: "People",
  agent: "Agents",
};

const SUBJECT_KINDS: readonly SubjectKind[] = ["team", "person", "agent"];

/** Subjects as the groups of a choice, one per kind, by name and type. */
export function subjectChoices(
  subjects: readonly Subject[],
  valueOf: (subject: Subject) => string,
): ChoiceGroup[] {
  return SUBJECT_KINDS.map((kind) => ({
    label: KIND_HEADING[kind],
    options: subjects
      .filter((subject) => subject.kind === kind)
      .map((subject) => ({
        value: valueOf(subject),
        label: `${subject.label} · ${KIND_LABEL[kind]}`,
      })),
  }));
}

/** What a subject GRN names, by its service type. */
function kindOf(grn: string): SubjectKind | "unknown" {
  switch (parseGrn(grn)?.serviceType) {
    case "group":
      return "team";
    case "user":
      return "person";
    case "agent":
      return "agent";
    default:
      return "unknown";
  }
}

/** A person's name, or what stands in for one. */
function personLabel(member: Member): string {
  return (
    member.nickname ?? `No name on record (${slugFromGrn(member.userGrn)})`
  );
}

/**
 * The subjects of one kind, or null where garam refused that list. An
 * agent carries no name of its own (#1125), so its identifier is its
 * label.
 */
function subjectsOf(catalogs: Catalogs, kind: SubjectKind): Subject[] | null {
  const subject = (grn: string, label: string): Subject => ({
    grn,
    kind,
    identifier: slugFromGrn(grn),
    label,
  });
  switch (kind) {
    case "team":
      return catalogs.teams.status === "ok"
        ? catalogs.teams.data.map((team) => subject(team.grn, team.name))
        : null;
    case "person":
      return catalogs.people.status === "ok"
        ? catalogs.people.data.map((person) =>
            subject(person.userGrn, personLabel(person)),
          )
        : null;
    case "agent":
      return catalogs.agents.status === "ok"
        ? catalogs.agents.data.map((agent) =>
            subject(agent.grn, slugFromGrn(agent.grn)),
          )
        : null;
  }
}

/** Every subject the caller can list: teams, then people, then agents. */
export function listSubjects(catalogs: Catalogs): Subject[] {
  return SUBJECT_KINDS.flatMap((kind) => subjectsOf(catalogs, kind) ?? []);
}

/** The kinds whose list garam refused the caller. */
export function refusedKinds(catalogs: Catalogs): SubjectKind[] {
  return SUBJECT_KINDS.filter((kind) => subjectsOf(catalogs, kind) === null);
}

/**
 * Reads an attachment's subject GRN against the organization's lists.
 * One no list holds stays visible, marked missing; one whose list was
 * refused is marked refused rather than missing.
 */
export function describeSubject(
  grn: string,
  catalogs: Catalogs,
): AttachedSubject {
  const identifier = slugFromGrn(grn);
  const kind = kindOf(grn);
  if (kind === "unknown") {
    return { grn, kind, identifier, label: null, lookup: "missing" };
  }
  const listed = subjectsOf(catalogs, kind);
  if (listed === null) {
    return {
      grn,
      kind,
      identifier,
      label: kind === "agent" ? identifier : null,
      lookup: "refused",
    };
  }
  const found = listed.find((subject) => subject.grn === grn);
  return found === undefined
    ? { grn, kind, identifier, label: null, lookup: "missing" }
    : { grn, kind, identifier, label: found.label, lookup: "found" };
}

/** How a subject is named in a sentence: its name, or what it is. */
export function subjectLabel(subject: AttachedSubject | Subject): string {
  if (subject.label !== null) return subject.label;
  return `the ${KIND_LABEL[subject.kind].toLowerCase()} ${subject.identifier}`;
}

/**
 * The query value addressing a subject on the Attachments view: its
 * kind and identifier, never its GRN (ADR-0011).
 */
export function subjectQuery(subject: Subject): string {
  return `${subject.kind}:${subject.identifier}`;
}

/** What a subject query resolved to. */
export type SubjectLookup =
  | { status: "none" }
  | { status: "found"; subject: Subject }
  | { status: "missing" }
  | { status: "refused"; kind: SubjectKind };

/** Resolves a `subject` query against the lists the caller can read. */
export function resolveSubjectQuery(
  value: string | undefined,
  catalogs: Catalogs,
): SubjectLookup {
  if (value === undefined || value === "") return { status: "none" };
  const separator = value.indexOf(":");
  const kind = SUBJECT_KINDS.find(
    (candidate) => candidate === value.slice(0, separator),
  );
  if (separator < 0 || kind === undefined) return { status: "missing" };
  const identifier = value.slice(separator + 1);
  const listed = subjectsOf(catalogs, kind);
  if (listed === null) return { status: "refused", kind };
  const subject = listed.find(
    (candidate) => candidate.identifier === identifier,
  );
  return subject === undefined
    ? { status: "missing" }
    : { status: "found", subject };
}

/** A policy and what reading its attachments answered. */
export type PolicyScope = {
  policy: Policy;
  attachments: Catalog<PolicyAttachment>;
};

/** A team and what reading its members answered. */
export type TeamMembership = { team: Group; members: Catalog<GroupMember> };

/**
 * Which policies name a subject, by where the naming comes from. This
 * is what is stored, not what a check decides: roles grant too, an
 * explicit Deny wins over every Allow, and Conditions are evaluated by
 * nothing (`docs/architecture/authz.md`).
 */
export type Inspection = {
  direct: Policy[];
  /** Absent for a team, since a team belongs to no team. */
  inherited?: {
    via: { policy: Policy; team: Group }[];
    /** Null where the teams list itself was refused. */
    uncheckedTeams: Group[] | null;
  };
  orgWide: Policy[];
  /** Policies whose attachments were refused: their scope is unknown. */
  unread: Policy[];
};

/**
 * Sorts the organization's policies by how each names `subject`. A
 * policy whose attachments were not read is in none of the three
 * lists — neither attached nor organization-wide is known of it — and
 * a team whose members were not read is named as unchecked where a
 * policy is attached to it.
 */
export function inspectSubject(
  subject: Subject,
  scopes: readonly PolicyScope[],
  teams: Catalog<TeamMembership>,
): Inspection {
  const read = scopes.flatMap((scope) =>
    scope.attachments.status === "ok"
      ? [{ policy: scope.policy, attachments: scope.attachments.data }]
      : [],
  );
  const direct = read
    .filter(({ attachments }) =>
      attachments.some((attachment) => attachment.subjectGrn === subject.grn),
    )
    .map(({ policy }) => policy);
  const orgWide = read
    .filter(
      ({ policy, attachments }) =>
        policy.status === "active" && attachments.length === 0,
    )
    .map(({ policy }) => policy);
  const unread = scopes
    .filter((scope) => scope.attachments.status === "forbidden")
    .map((scope) => scope.policy);
  if (subject.kind === "team") return { direct, orgWide, unread };

  const attachedTo = (team: Group) =>
    read.filter(({ attachments }) =>
      attachments.some((attachment) => attachment.subjectGrn === team.grn),
    );
  if (teams.status === "forbidden") {
    return {
      direct,
      inherited: { via: [], uncheckedTeams: null },
      orgWide,
      unread,
    };
  }
  const via = teams.data
    .filter(
      ({ members }) =>
        members.status === "ok" &&
        members.data.some((member) => member.memberGrn === subject.grn),
    )
    .flatMap(({ team }) =>
      attachedTo(team).map(({ policy }) => ({ policy, team })),
    );
  const uncheckedTeams = teams.data
    .filter(
      ({ team, members }) =>
        members.status === "forbidden" && attachedTo(team).length > 0,
    )
    .map(({ team }) => team);
  return { direct, inherited: { via, uncheckedTeams }, orgWide, unread };
}

/**
 * Who a policy reaches while active, as far as the page read: the
 * subjects it is attached to, the whole organization where it has
 * none, or unknown where its attachments were refused.
 */
export type Reach =
  | { kind: "attached"; subjects: AttachedSubject[] }
  | { kind: "org-wide" }
  | { kind: "unknown" };

/** The reach of a policy whose attachments read as `attachments`. */
export function reachOf(
  attachments: Catalog<PolicyAttachment>,
  catalogs: Catalogs,
): Reach {
  if (attachments.status === "forbidden") return { kind: "unknown" };
  if (attachments.data.length === 0) return { kind: "org-wide" };
  return {
    kind: "attached",
    subjects: attachments.data.map((attachment) =>
      describeSubject(attachment.subjectGrn, catalogs),
    ),
  };
}

/** The reach as the one sentence a confirmation states. */
export function reachSentence(reach: Reach): string {
  switch (reach.kind) {
    case "attached":
      return `Applies to ${reach.subjects
        .map(
          (subject) => `${subjectLabel(subject)} (${KIND_LABEL[subject.kind]})`,
        )
        .join(", ")}.`;
    case "org-wide":
      return "No attachments: applies to everyone in this organization.";
    case "unknown":
      return "Its attachments are not listed to you, so it may apply to everyone in this organization.";
  }
}
