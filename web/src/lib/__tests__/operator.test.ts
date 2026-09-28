import { describe, expect, it } from "vitest";

import {
  parseOperatorDescription,
  parseOperatorIdentifier,
  parseOperatorName,
} from "@/lib/operator";

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

describe("parseOperatorName", () => {
  it("trims the name and refuses one that is only space", () => {
    expect(parseOperatorName("  Field one  ")).toEqual({
      status: "ok",
      value: "Field one",
    });
    expect(parseOperatorName("   ")).toMatchObject({
      status: "error",
      message: "Give the operator a name.",
    });
  });

  it("counts characters as the API does: 100 astral characters are 100", () => {
    // Each of these is two UTF-16 units, so a count by `length` would
    // refuse the first as 200; it is the control for the second.
    expect(parseOperatorName("😀".repeat(100)).status).toBe("ok");
    expect(parseOperatorName("😀".repeat(101))).toMatchObject({
      status: "error",
      message:
        "An operator's name holds at most 100 characters; this one holds 101.",
    });
  });
});

describe("NUL in an operator's details", () => {
  it("refuses NUL in a name or description, and takes the same text without it", () => {
    // garam's text columns cannot hold NUL, so the API refuses it; the
    // text beside it without the character is the control.
    expect(parseOperatorName("a\0b")).toMatchObject({ status: "error" });
    expect(parseOperatorDescription("a\0b")).toMatchObject({
      status: "error",
    });
    expect(parseOperatorName("ab").status).toBe("ok");
    expect(parseOperatorDescription("ab").status).toBe("ok");
  });
});

describe("parseOperatorDescription", () => {
  it("takes an empty description, which is none", () => {
    expect(parseOperatorDescription("  ")).toEqual({ status: "ok", value: "" });
  });

  it("refuses past 1000 characters and takes 1000", () => {
    expect(parseOperatorDescription("a".repeat(1000)).status).toBe("ok");
    expect(parseOperatorDescription("a".repeat(1001))).toMatchObject({
      status: "error",
      message:
        "A description holds at most 1000 characters; this one holds 1001.",
    });
  });
});
