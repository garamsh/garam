import { parseGrn } from "@/lib/grn";

/**
 * A list a page looks an organization's subjects up in, or the fact
 * that garam refused the caller that list. A refused list names no one
 * and proves no one absent.
 */
export type Catalog<T> =
  { status: "ok"; data: readonly T[] } | { status: "forbidden" };

/**
 * What looking a subject up found: it, no such subject in the
 * organization, or nothing, because the list it would be in was
 * refused to the caller.
 */
export type Lookup = "found" | "missing" | "refused";

/** What a team can hold or a policy can be attached to. */
export type SubjectKind = "team" | "person" | "agent";

/** What a subject GRN names, by its service type. */
export function kindFromGrn(grn: string): SubjectKind | "unknown" {
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

/** The word a subject's type is shown as. */
export const KIND_LABEL: Record<SubjectKind | "unknown", string> = {
  team: "Team",
  person: "Person",
  agent: "Agent",
  unknown: "Unknown",
};
