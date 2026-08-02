import { describe, expect, it } from "vitest";

import { slugFromGrn } from "@/features/orgs/grn";

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

  it.each([
    "grn:root:default:org",
    "grn:root:default:org:acme:extra",
    "arn:root:default:org:acme",
    "",
  ])("rejects %j", (malformed) => {
    expect(() => slugFromGrn(malformed)).toThrow(/malformed GRN/);
  });
});
