import { describe, expect, it } from "vitest";

import {
  consoleCrumbs,
  parseConsolePath,
} from "@/app/console/_components/navigation";

const orgs = [{ slug: "acme", name: "Acme" }];

describe("parseConsolePath", () => {
  it("reads the organizations list", () => {
    expect(parseConsolePath("/console/orgs")).toEqual({ kind: "orgs" });
  });

  it("reads the create form", () => {
    expect(parseConsolePath("/console/orgs/new")).toEqual({ kind: "new-org" });
  });

  it("reads an organization's overview", () => {
    const location = parseConsolePath("/console/orgs/acme");

    expect(location.kind).toBe("org");
    expect(location).toMatchObject({ org: "acme" });
    expect(location.kind === "org" && location.section.label).toBe("Overview");
  });

  it.each([
    "teams",
    "people",
    "agents",
    "projects",
    "operators",
    "consents",
    "permissions",
  ])("reads the %s section", (segment) => {
    const location = parseConsolePath(`/console/orgs/acme/${segment}`);

    expect(location.kind === "org" && location.section.segment).toBe(segment);
  });

  it("reads a team's own page, and the page that creates one", () => {
    expect(parseConsolePath("/console/orgs/acme/teams/eng")).toMatchObject({
      kind: "org",
      org: "acme",
      section: { segment: "teams" },
      item: "eng",
    });
    expect(parseConsolePath("/console/orgs/acme/teams/new")).toMatchObject({
      item: "new",
    });
  });

  it("reads a policy's page and the Attachments view of Permissions", () => {
    expect(
      parseConsolePath("/console/orgs/acme/permissions/9f2ac1b4"),
    ).toMatchObject({
      section: { segment: "permissions" },
      item: "9f2ac1b4",
    });
    expect(
      parseConsolePath("/console/orgs/acme/permissions/attachments"),
    ).toMatchObject({
      section: { segment: "permissions" },
      item: "attachments",
    });
  });

  it("reads an invitation", () => {
    expect(parseConsolePath("/console/invitations/token")).toEqual({
      kind: "invitation",
    });
  });

  it.each([
    "/login",
    "/console/orgs/acme/nothing",
    // The section's former address redirects before the shell reads it.
    "/console/orgs/acme/policies",
    // A section without item pages has nothing below it.
    "/console/orgs/acme/people/deeper",
    "/console/orgs/acme/teams/eng/deeper",
  ])("does not place %s", (pathname) => {
    expect(parseConsolePath(pathname)).toEqual({ kind: "unknown" });
  });
});

describe("consoleCrumbs", () => {
  it("names the organization and the section", () => {
    const crumbs = consoleCrumbs(
      parseConsolePath("/console/orgs/acme/people"),
      orgs,
    );

    expect(crumbs).toEqual([
      { label: "Organizations", href: "/console/orgs" },
      { label: "Acme", href: "/console/orgs/acme" },
      { label: "People" },
    ]);
  });

  it("links the section above a team and names the team", () => {
    const crumbs = consoleCrumbs(
      parseConsolePath("/console/orgs/acme/teams/eng"),
      orgs,
      "Engineering",
    );

    expect(crumbs).toEqual([
      { label: "Organizations", href: "/console/orgs" },
      { label: "Acme", href: "/console/orgs/acme" },
      { label: "Teams", href: "/console/orgs/acme/teams" },
      { label: "Engineering" },
    ]);
  });

  it("calls a team page 'Team' until the page has named it", () => {
    const crumbs = consoleCrumbs(
      parseConsolePath("/console/orgs/acme/teams/eng"),
      orgs,
    );

    expect(crumbs.at(-1)).toEqual({ label: "Team" });
  });

  it("names the page that creates a team", () => {
    const crumbs = consoleCrumbs(
      parseConsolePath("/console/orgs/acme/teams/new"),
      orgs,
      "Engineering",
    );

    expect(crumbs.at(-1)).toEqual({ label: "New team" });
  });

  it("names the Attachments view by its label, not as a policy", () => {
    const crumbs = consoleCrumbs(
      parseConsolePath("/console/orgs/acme/permissions/attachments"),
      orgs,
    );

    expect(crumbs.slice(2)).toEqual([
      { label: "Permissions", href: "/console/orgs/acme/permissions" },
      { label: "Attachments" },
    ]);
  });

  it("names a policy's page and the page that creates one", () => {
    const policy = parseConsolePath("/console/orgs/acme/permissions/9f2ac1b4");

    expect(consoleCrumbs(policy, orgs).at(-1)).toEqual({ label: "Policy" });
    expect(consoleCrumbs(policy, orgs, "Deny exports").at(-1)).toEqual({
      label: "Deny exports",
    });
    expect(
      consoleCrumbs(
        parseConsolePath("/console/orgs/acme/permissions/new"),
        orgs,
      ).at(-1),
    ).toEqual({ label: "New policy" });
  });

  it("names an operator's page, the identifier new included", () => {
    const operator = parseConsolePath("/console/orgs/acme/operators/field-one");

    expect(consoleCrumbs(operator, orgs).at(-2)).toEqual({
      label: "Operators",
      href: "/console/orgs/acme/operators",
    });
    expect(consoleCrumbs(operator, orgs).at(-1)).toEqual({ label: "Operator" });
    expect(consoleCrumbs(operator, orgs, "Field cluster").at(-1)).toEqual({
      label: "Field cluster",
    });
    // Operators has no create page: `new` is a valid identifier, so it
    // is an operator's page and takes the name that page reads.
    const named = parseConsolePath("/console/orgs/acme/operators/new");
    expect(consoleCrumbs(named, orgs, "New cluster").at(-1)).toEqual({
      label: "New cluster",
    });
    // The control: a section with a create page still names it.
    expect(
      consoleCrumbs(
        parseConsolePath("/console/orgs/acme/teams/new"),
        orgs,
        "x",
      ).at(-1),
    ).toEqual({ label: "New team" });
  });

  it("falls back to the slug for an organization not in the list", () => {
    const crumbs = consoleCrumbs(parseConsolePath("/console/orgs/other"), []);

    expect(crumbs.map((crumb) => crumb.label)).toEqual([
      "Organizations",
      "other",
      "Overview",
    ]);
  });

  it("names the invitation page (ADR-0011: it renders in the shell)", () => {
    const crumbs = consoleCrumbs(
      parseConsolePath("/console/invitations/token"),
      orgs,
    );

    expect(crumbs).toEqual([
      { label: "Organizations", href: "/console/orgs" },
      { label: "Invitation" },
    ]);
  });

  it("leaves the current page unlinked", () => {
    const crumbs = consoleCrumbs(parseConsolePath("/console/orgs"), orgs);

    expect(crumbs).toEqual([{ label: "Organizations" }]);
  });
});
