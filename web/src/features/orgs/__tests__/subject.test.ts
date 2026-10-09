import { describe, expect, it } from "vitest";

import { kindFromGrn } from "@/features/orgs/subject";

describe("kindFromGrn", () => {
  it.each([
    ["grn:acme:default:group:eng", "team"],
    ["grn:acme:default:user:alice", "person"],
    ["grn:acme:default:agent:bot-1", "agent"],
  ])("reads %s as a %s", (grn, kind) => {
    expect(kindFromGrn(grn)).toBe(kind);
  });

  it.each([
    "grn:acme:default:policy:a1b2c3d4",
    "grn:root:default:org:acme",
    "grn:acme:default:agent",
    "",
  ])("reads %j as of no known kind", (value) => {
    expect(kindFromGrn(value)).toBe("unknown");
  });
});
