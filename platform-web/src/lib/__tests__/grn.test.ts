import { describe, expect, it } from "vitest";

import { parseGrn, projectsPattern, slugFromGrn } from "@/lib/grn";

const malformed = [
  "grn:root/iam/org",
  "grn:root/iam/org/acme/extra",
  "grn:root/iam/org/Acme",
  "grn:root/iam/org/acme#",
  `grn:acme/iam/user/${"a".repeat(513)}`,
  "arn:root/iam/org/acme",
  "",
];

describe("slugFromGrn", () => {
  it("returns the identifier segment", () => {
    expect(slugFromGrn("grn:root/iam/org/acme")).toBe("acme");
    expect(slugFromGrn("grn:acme/iam/group/eng")).toBe("eng");
  });

  it("keeps identifiers that are not slug-shaped", () => {
    expect(slugFromGrn("grn:root/iam/user/117421510527410544321")).toBe(
      "117421510527410544321",
    );
  });

  it.each(malformed)("rejects %j", (value) => {
    // The control: a well-formed GRN still gives up its segment, so the
    // throw beside it is this string and not a function that throws on
    // everything.
    expect(slugFromGrn("grn:root/iam/org/acme")).toBe("acme");
    expect(() => slugFromGrn(value)).toThrow(/malformed GRN/);
  });

  it("refuses the five-segment form", () => {
    expect(() =>
      slugFromGrn(["grn", "acme", "default", "user", "ada"].join(":")),
    ).toThrow(/malformed GRN/);
  });
});

describe("parseGrn", () => {
  it("returns every part the GRN carries", () => {
    expect(parseGrn("grn:acme/iam/user/alice")).toEqual({
      organization: "acme",
      service: "iam",
      ancestors: "",
      serviceType: "user",
      identifier: "alice",
      locator: null,
    });
  });

  it("splits ancestors and a locator off the resource's own pair", () => {
    expect(
      parseGrn("grn:acme/codehub/project/web/repo/api#refs/heads/main"),
    ).toEqual({
      organization: "acme",
      service: "codehub",
      ancestors: "project/web",
      serviceType: "repo",
      identifier: "api",
      locator: "refs/heads/main",
    });
  });

  it.each(malformed)("returns null for %j", (value) => {
    // The control: a well-formed GRN parses, so the null beside it is
    // this string's shape and not a parse that reads nothing.
    expect(parseGrn("grn:root/iam/org/acme")).not.toBeNull();
    expect(parseGrn(value)).toBeNull();
  });
});

describe("projectsPattern", () => {
  it("matches the projects of one organization", () => {
    expect(projectsPattern("acme")).toBe("grn:acme/iam/project/*");
  });
});
