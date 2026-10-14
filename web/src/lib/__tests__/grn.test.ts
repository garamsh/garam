import { describe, expect, it } from "vitest";

import { parseGrn, slugFromGrn } from "@/lib/grn";

const malformed = [
  "grn:root:default:org",
  "grn:root:default:org:acme:extra",
  "arn:root:default:org:acme",
  "",
];

describe("slugFromGrn", () => {
  it("returns the identifier segment", () => {
    expect(slugFromGrn("grn:root:default:org:acme")).toBe("acme");
    expect(slugFromGrn("grn:acme:default:group:eng")).toBe("eng");
  });

  it("keeps identifiers that are not slug-shaped", () => {
    expect(slugFromGrn("grn:root:default:user:117421510527410544321")).toBe(
      "117421510527410544321",
    );
  });

  it.each(malformed)("rejects %j", (value) => {
    // The control: a well-formed GRN still gives up its segment, so the
    // throw beside it is this string and not a function that throws on
    // everything.
    expect(slugFromGrn("grn:root:default:org:acme")).toBe("acme");
    expect(() => slugFromGrn(value)).toThrow(/malformed GRN/);
  });
});

describe("parseGrn", () => {
  it("returns every segment the GRN carries", () => {
    expect(parseGrn("grn:acme:frontend:user:alice")).toEqual({
      organization: "acme",
      project: "frontend",
      serviceType: "user",
      identifier: "alice",
    });
  });

  it.each(malformed)("returns null for %j", (value) => {
    // The control: a well-formed GRN parses, so the null beside it is
    // this string's shape and not a parse that reads nothing.
    expect(parseGrn("grn:root:default:org:acme")).not.toBeNull();
    expect(parseGrn(value)).toBeNull();
  });
});
