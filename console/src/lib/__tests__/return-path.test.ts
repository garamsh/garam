import { readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import {
  invitationTokenOf,
  safeReturnPath,
  withReturnPath,
} from "@/lib/return-path";

/**
 * The vectors the Go rule is tested against too
 * (`platform/internal/api/auth_return_path_test.go`), so a path one
 * rule accepts and the other drops fails on one side. A `goOnly` vector
 * is raw bytes a JavaScript string cannot hold.
 */
const VECTORS: {
  name: string;
  input?: string;
  accepted: boolean;
  goOnly?: boolean;
}[] = JSON.parse(
  readFileSync(
    join(
      import.meta.dirname,
      "../../../../platform/internal/api/testdata/return-path-vectors.json",
    ),
    "utf8",
  ),
).vectors;

describe("safeReturnPath", () => {
  it("reads the shared vectors", () => {
    expect(VECTORS.filter((vector) => !vector.goOnly).length).toBeGreaterThan(
      10,
    );
  });

  it.each(VECTORS.filter((vector) => !vector.goOnly))(
    "holds the shared vector: $name",
    ({ input, accepted }) => {
      expect(safeReturnPath(input)).toBe(accepted ? input : null);
    },
  );

  it.each([
    ["no value", null],
    ["a lone surrogate, which is no UTF-8 at all", "/console/\ud800"],
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
