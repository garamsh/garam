import { describe, expect, it } from "vitest";

import {
  CONSOLE_NAV,
  CONSOLE_SECTIONS,
  consoleCrumbs,
  parseConsolePath,
  resolveNavEntry,
} from "@/app/console/_components/navigation";

const orgs = [{ slug: "acme", name: "Acme" }];

describe("parseConsolePath", () => {
  it("reads the organizations list", () => {
    expect(parseConsolePath("/console/orgs")).toEqual({ kind: "orgs" });
  });

  it("reads the create form", () => {
    expect(parseConsolePath("/console/orgs/new")).toEqual({ kind: "new-org" });
  });

  it("reads an organization's Home", () => {
    const location = parseConsolePath("/console/orgs/acme");

    expect(location.kind).toBe("org");
    expect(location).toMatchObject({ org: "acme" });
    expect(location.kind === "org" && location.section.label).toBe("Home");
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

  it("reads an agent's page and a template's page inside Agents", () => {
    expect(
      parseConsolePath("/console/orgs/acme/agents/9f2ac1b40d8e7a35"),
    ).toMatchObject({
      section: { segment: "agents" },
      item: "9f2ac1b40d8e7a35",
    });
    expect(
      parseConsolePath("/console/orgs/acme/agents/templates/7c1e2a9b"),
    ).toMatchObject({
      section: { segment: "agents" },
      item: "templates",
      viewItem: "7c1e2a9b",
    });
    // A section's entry has no pages below it, so the extra segment is
    // not read as a view's item.
    expect(parseConsolePath("/console/orgs/acme/teams/eng/members")).toEqual({
      kind: "unknown",
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

  it("links Agents and Templates above a template and names it", () => {
    const template = parseConsolePath(
      "/console/orgs/acme/agents/templates/7c1e2a9b",
    );

    expect(consoleCrumbs(template, orgs, "Researcher").slice(2)).toEqual([
      { label: "Agents", href: "/console/orgs/acme/agents" },
      { label: "Templates", href: "/console/orgs/acme/agents/templates" },
      { label: "Researcher" },
    ]);
    expect(consoleCrumbs(template, orgs).at(-1)).toEqual({
      label: "Template",
    });
    // Garam's templates are retired read-only, so `new` names no create
    // page and reads as an identifier (ADR-0088).
    expect(
      consoleCrumbs(
        parseConsolePath("/console/orgs/acme/agents/templates/new"),
        orgs,
      ).at(-1),
    ).toEqual({ label: "Template" });
    expect(
      consoleCrumbs(
        parseConsolePath("/console/orgs/acme/agents/templates"),
        orgs,
      ).slice(2),
    ).toEqual([
      { label: "Agents", href: "/console/orgs/acme/agents" },
      { label: "Templates" },
    ]);
    expect(
      consoleCrumbs(parseConsolePath("/console/orgs/acme/agents/new"), orgs).at(
        -1,
      ),
    ).toEqual({ label: "New agent" });
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
      "Home",
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

describe("CONSOLE_NAV", () => {
  it("lists every section exactly once", () => {
    const listed = CONSOLE_NAV.flatMap((group) =>
      group.entries
        .filter((entry) => entry.view === undefined)
        .map((entry) => entry.section),
    );

    expect([...listed].sort()).toEqual(
      CONSOLE_SECTIONS.map((section) => section.segment).sort(),
    );
  });

  it("gates every entry but Home on a read", () => {
    const ungated = CONSOLE_NAV.flatMap((group) => group.entries)
      .map(resolveNavEntry)
      .filter((entry) => entry !== undefined && entry.read === undefined)
      .map((entry) => entry?.label);

    expect(ungated).toEqual(["Home"]);
  });

  it("resolves every entry to a section or a listed view", () => {
    for (const group of CONSOLE_NAV) {
      for (const entry of group.entries) {
        expect(resolveNavEntry(entry)).toBeDefined();
      }
    }
  });

  it("resolves no view the sidebar does not list", () => {
    // Attachments is a view of Permissions with no sidebar entry; the
    // control is Templates, which has one.
    expect(
      resolveNavEntry({ section: "permissions", view: "attachments" }),
    ).toBeUndefined();
    expect(
      resolveNavEntry({ section: "agents", view: "templates" })?.segment,
    ).toBe("agents/templates");
  });
});

describe("an operator's setup page", () => {
  it("reads as a page of the operator, not as an identifier", () => {
    const location = parseConsolePath(
      "/console/orgs/acme/operators/field-one/connect",
    );

    expect(location).toMatchObject({
      kind: "org",
      item: "field-one",
      itemPage: { segment: "connect", label: "Connect" },
    });
  });

  it("links the operator above its setup and names both", () => {
    const crumbs = consoleCrumbs(
      parseConsolePath("/console/orgs/acme/operators/field-one/connect"),
      orgs,
      "Field one",
    );

    expect(crumbs.slice(2)).toEqual([
      { label: "Operators", href: "/console/orgs/acme/operators" },
      { label: "Field one", href: "/console/orgs/acme/operators/field-one" },
      { label: "Connect" },
    ]);
  });

  it("reads a page no operator declares as unknown", () => {
    // Control: only the declared segment is a page of the operator.
    expect(
      parseConsolePath("/console/orgs/acme/operators/field-one/elsewhere"),
    ).toEqual({ kind: "unknown" });
  });
});
