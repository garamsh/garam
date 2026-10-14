import { describe, expect, it } from "vitest";

import {
  invitationTokenOf,
  safeReturnPath,
  withReturnPath,
} from "@/lib/return-path";

describe("safeReturnPath", () => {
  it.each([
    "/console",
    "/console/orgs/acme/teams",
    "/console/invitations/tok-123",
    "/console/orgs/acme/permissions/attachments?subject=team",
  ])("returns to a console path (%s)", (path) => {
    expect(safeReturnPath(path)).toBe(path);
  });

  it.each([
    ["another origin", "https://evil.example/console"],
    ["a protocol-relative host", "//evil.example/console"],
    ["a backslash a browser reads as a slash", "/console/\\\\evil.example"],
    ["a control character", "/console/\torgs"],
    ["a public page", "/register"],
    ["a prefix that is not the console segment", "/consoleevil"],
    ["no value", null],
  ])("drops %s", (_, value) => {
    expect(safeReturnPath(value)).toBeNull();
  });
});

describe("invitationTokenOf", () => {
  it("reads the token an invitation path carries", () => {
    expect(invitationTokenOf("/console/invitations/tok-123")).toBe("tok-123");
  });

  it.each([
    "/console/orgs",
    "/console/invitations/",
    "/console/invitations/tok-123/accept",
    "/console/invitations/tok-123?x=1",
  ])("reads none from %s", (path) => {
    expect(invitationTokenOf(path)).toBeNull();
  });
});

describe("withReturnPath", () => {
  it("attaches the path as the next parameter, and nothing without one", () => {
    expect(withReturnPath("/register", "/console/invitations/t")).toBe(
      "/register?next=%2Fconsole%2Finvitations%2Ft",
    );
    expect(withReturnPath("/register", null)).toBe("/register");
  });
});
