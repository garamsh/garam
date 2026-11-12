import { describe, expect, it } from "vitest";

import { identifierOf } from "@/lib/grn";

describe("identifierOf", () => {
  it("returns the last id of a GRN", () => {
    expect(identifierOf("grn:root/iam/user/ada")).toBe("ada");
    expect(identifierOf("grn:acme/codehub/project/web/repo/api")).toBe("api");
  });

  it("leaves a locator out", () => {
    expect(identifierOf("grn:acme/cowork/page/k3v7#a/b")).toBe("k3v7");
  });

  it("returns what is not a GRN as it is", () => {
    expect(identifierOf("ada")).toBe("ada");
    expect(identifierOf("")).toBe("");
  });
});
