import { describe, expect, it } from "vitest";

import {
  memberLabel,
  memberName,
  type TeamMember,
} from "@/app/console/orgs/[org]/teams/[team]/_components/team-member";

const ID = "1c3d5e7f90a2b4c6";

function member(overrides: Partial<TeamMember>): TeamMember {
  return {
    grn: `grn:acme:default:agent:${ID}`,
    identifier: ID,
    kind: "agent",
    label: null,
    lookup: "found",
    nameRead: true,
    ...overrides,
  };
}

describe("team member names", () => {
  it.each([
    ["an agent no definition names", {}, "Unnamed agent", "an unnamed agent"],
    [
      "an agent whose definitions were not read",
      { nameRead: false },
      "Name not available",
      "an agent whose name is not available",
    ],
    [
      "an agent the organization no longer holds",
      { lookup: "missing" as const },
      "Unknown agent",
      "an agent no longer in this organization",
    ],
    [
      "a person with no name on record",
      { kind: "person" as const },
      "No name on record",
      "a person with no name on record",
    ],
    [
      "a person whose list was refused",
      { kind: "person" as const, lookup: "refused" as const },
      "Name not available",
      "a person whose name is not available",
    ],
    [
      "an identity of neither type",
      { kind: "unknown" as const, lookup: "missing" as const },
      "Unknown identity",
      "an unknown member",
    ],
  ])(
    "calls %s by what it is, never by its identifier",
    (_, overrides, name, label) => {
      const subject = member(overrides);

      expect(memberName(subject)).toBe(name);
      expect(memberLabel(subject)).toBe(label);
      expect(memberLabel(subject)).not.toContain(ID);
    },
  );

  it("calls a named member by its name alone", () => {
    const scout = member({ label: "Scout" });

    expect(memberName(scout)).toBe("Scout");
    expect(memberLabel(scout)).toBe("Scout");
  });
});
