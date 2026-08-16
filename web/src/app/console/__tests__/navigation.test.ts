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

  it.each(["groups", "projects", "members", "policies"])(
    "reads the %s section",
    (segment) => {
      const location = parseConsolePath(`/console/orgs/acme/${segment}`);

      expect(location.kind === "org" && location.section.segment).toBe(segment);
    },
  );

  it("reads an invitation", () => {
    expect(parseConsolePath("/console/invitations/token")).toEqual({
      kind: "invitation",
    });
  });

  it.each([
    "/login",
    "/console/orgs/acme/nothing",
    "/console/orgs/acme/groups/deeper",
  ])("does not place %s", (pathname) => {
    expect(parseConsolePath(pathname)).toEqual({ kind: "unknown" });
  });
});

describe("consoleCrumbs", () => {
  it("names the organization and the section", () => {
    const crumbs = consoleCrumbs(
      parseConsolePath("/console/orgs/acme/members"),
      orgs,
    );

    expect(crumbs).toEqual([
      { label: "Organizations", href: "/console/orgs" },
      { label: "Acme", href: "/console/orgs/acme" },
      { label: "Members" },
    ]);
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
