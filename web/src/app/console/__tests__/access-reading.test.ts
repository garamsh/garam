import { describe, expect, it } from "vitest";

import { grantLines } from "@/app/console/_components/access-reading";

describe("reading access from the root down", () => {
  it("lists the organization first, then project, team and the caller, and an unreadable policy last", () => {
    const lines = grantLines([
      { effect: "Allow", source: "unreadable-policy" },
      {
        effect: "Allow",
        source: "policy",
        policy: "ops",
        attachment: "caller",
      },
      { effect: "Allow", source: "group-role", group: "eng", roles: ["admin"] },
      { effect: "Allow", source: "org-role", roles: ["member"] },
      {
        effect: "Allow",
        source: "policy",
        policy: "base",
        sid: "read",
        attachment: "organization",
      },
    ]);

    expect(lines.map((line) => line.text)).toEqual([
      "Your organization role member",
      "Policy base, statement read, applied to the whole organization",
      "Team eng's role admin",
      "Policy ops, attached to you",
      "A policy you can't read",
    ]);
  });

  it("keeps garam's order inside one scope", () => {
    const lines = grantLines([
      {
        effect: "Deny",
        source: "policy",
        policy: "b",
        attachment: "group",
        group: "eng",
      },
      {
        effect: "Allow",
        source: "group-role",
        group: "ops",
        roles: ["member"],
      },
    ]);

    expect(lines.map((line) => line.text)).toEqual([
      "Policy b, attached to team eng",
      "Team ops's role member",
    ]);
  });
});
