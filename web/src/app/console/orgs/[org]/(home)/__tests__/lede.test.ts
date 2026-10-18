import { describe, expect, it } from "vitest";

import { homeLede } from "@/app/console/orgs/[org]/(home)/_components/lede";

const link = (text: string) => ({ text, href: `/x/${text}` });

/** The lede as it reads, a link shown as [name]. */
function read(parts: ReturnType<typeof homeLede>): string {
  return parts
    .map((part) => (typeof part === "string" ? part : `[${part.text}]`))
    .join("");
}

describe("Home's lede", () => {
  it("names the caller's teams and what needs a look, each linked", () => {
    const parts = homeLede({
      teams: [link("Support"), link("Platform")],
      attention: [link("scout")],
    });

    expect(read(parts)).toBe(
      "Nothing is waiting on you. You are in [Support] and [Platform]. [scout] needs a look.",
    );
  });

  it("counts what it does not name past three", () => {
    const parts = homeLede({
      teams: [],
      attention: ["a", "b", "c", "d", "e"].map(link),
    });

    expect(read(parts)).toBe(
      "Nothing is waiting on you. You are in no team here. These need a look: [a], [b], [c] and 2 more.",
    );
  });

  it("says nothing it could not read, rather than none", () => {
    // A reader who can see little: neither teams nor anything failing
    // was read, so neither is claimed.
    expect(read(homeLede({ teams: null, attention: null }))).toBe(
      "Nothing is waiting on you.",
    );
    // Control: read and empty is said.
    expect(read(homeLede({ teams: [], attention: [] }))).toBe(
      "Nothing is waiting on you. You are in no team here. Nothing you can read is failing.",
    );
  });
});
