import { describe, expect, it } from "vitest";

import { parseOperatorIdentifier } from "@/lib/operator";

describe("parseOperatorIdentifier", () => {
  it("takes the identifier the console addresses an operator by", () => {
    expect(parseOperatorIdentifier("field-one")).toEqual({
      status: "ok",
      value: "field-one",
    });
  });

  it("refuses a segment carrying a colon, and takes the same one without it", () => {
    // The control: what is refused is the colon, not the rest of the
    // string — the accepted case stops being accepted only once it
    // carries one.
    expect(parseOperatorIdentifier("acmefield").status).toBe("ok");
    expect(parseOperatorIdentifier("acme:field")).toMatchObject({
      status: "error",
      message: expect.stringContaining("lowercase letters"),
    });
  });

  it("refuses an uppercase identifier and names the character rule", () => {
    expect(parseOperatorIdentifier("Field-One")).toMatchObject({
      status: "error",
      message:
        "An operator identifier holds only lowercase letters, digits and hyphens.",
    });
  });

  it("refuses past 64 characters and takes 64", () => {
    expect(parseOperatorIdentifier("a".repeat(64)).status).toBe("ok");
    expect(parseOperatorIdentifier("a".repeat(65))).toMatchObject({
      status: "error",
      message:
        "An operator identifier holds at most 64 characters; this one holds 65.",
    });
  });

  it("asks for an operator rather than refusing an empty field as invalid", () => {
    expect(parseOperatorIdentifier("  ")).toEqual({
      status: "error",
      message: "Name the operator.",
    });
  });
});
