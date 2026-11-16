import { describe, expect, it } from "vitest";

import { searchOrgObjects } from "@/features/orgs/api/search-org-objects";

describe("searchOrgObjects", () => {
  it("lists what the palette opens and leaves out the earlier engine's policies", async () => {
    // "deny" matches only Acme's policy of the earlier engine, which
    // the permissions pages no longer open.
    const denied = await searchOrgObjects("acme", "deny");
    if (denied.status !== "ok") throw new Error("the search was unanswered");
    expect(denied.found.map(({ kind }) => kind)).toEqual([
      "person",
      "agent",
      "team",
      "project",
    ]);
    expect(denied.found.flatMap(({ objects }) => objects)).toEqual([]);

    // The control: a query that matches a listed kind is answered.
    const engineers = await searchOrgObjects("acme", "eng");
    if (engineers.status !== "ok") throw new Error("the search was unanswered");
    expect(
      engineers.found.find(({ kind }) => kind === "team")?.objects,
    ).toEqual([{ name: "Engineering", identifier: "eng" }]);
  });
});
