import { describe, expect, it } from "vitest";

import { grantLines } from "@/app/console/_components/access-reading";

describe("reading access from the root down", () => {
  it("lists the organization first, then project and team, and the stored and system policies last", () => {
    const lines = grantLines([
      { kind: "system-policy", policy: "tenant-isolation" },
      { kind: "permit", policy: "p-ops" },
      { kind: "role-link", role: "admin", path: "team", team: "eng" },
      { kind: "role-link", role: "member", path: "organization" },
      { kind: "role-link", role: "owner", path: "project", project: "web" },
    ]);

    expect(lines.map((line) => line.text)).toEqual([
      "Your organization role member",
      "Project web's role owner",
      "Team eng's role admin",
      "System policy tenant-isolation",
      "Permit p-ops",
    ]);
  });

  it("keeps the order given inside one scope", () => {
    const lines = grantLines([
      { kind: "guardrail", policy: "p-b" },
      { kind: "role-link", role: "member", path: "team", team: "ops" },
      { kind: "role-link", role: "admin", path: "team", team: "eng" },
    ]);

    expect(lines.map((line) => line.text)).toEqual([
      "Team ops's role member",
      "Team eng's role admin",
      "Guardrail p-b",
    ]);
  });

  it("names a policy by the name a caller who may read it is given, and carries its text", () => {
    const [named, unnamed] = grantLines([
      {
        kind: "guardrail",
        policy: "p-1",
        name: "no-invites",
        text: "forbid (principal, action, resource);",
      },
      { kind: "guardrail", policy: "p-2" },
    ]);

    expect(named).toMatchObject({
      text: "Guardrail no-invites",
      detail: "forbid (principal, action, resource);",
    });
    // The control: a caller refused the read is told the identifier and
    // nothing more.
    expect(unnamed).toEqual({ scope: "policy", text: "Guardrail p-2" });
  });

  it("says what failed to evaluate, and a question refused without a policy", () => {
    const lines = grantLines([
      { kind: "error", policy: "p-3", error: "type error" },
      { kind: "error", error: "unknown action" },
    ]);

    expect(lines.map((line) => line.text)).toEqual([
      "Policy p-3 failed to evaluate: type error",
      "The question was refused: unknown action",
    ]);
  });

  it("names a failure and a refusal without inventing a message", () => {
    const lines = grantLines([
      { kind: "error", policy: "p-3" },
      { kind: "error" },
    ]);

    expect(lines.map((line) => line.text)).toEqual([
      "Policy p-3 failed to evaluate",
      "The question was refused",
    ]);
  });

  it("names a stored policy whose kind is withheld by its identifier alone", () => {
    const lines = grantLines([{ kind: "stored-policy", policy: "p-7" }]);

    expect(lines).toEqual([{ scope: "policy", text: "Policy p-7" }]);
  });
});
