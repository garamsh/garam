import { describe, expect, it } from "vitest";

import {
  AGENT_VALUES_MAX_ENTRIES,
  AGENT_VALUE_MAX_BYTES,
  parseAgentValues,
  parseOperatorIdentifier,
  valueBytes,
} from "@/lib/agent-definition";

function rows(count: number) {
  return Array.from({ length: count }, (_, index) => ({
    key: `key-${index}`,
    value: "x",
  }));
}

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
      message: "Name the operator that is to run this agent.",
    });
  });
});

describe("parseAgentValues", () => {
  it("keeps the keys it is given without reading any of them", () => {
    expect(
      parseAgentValues([
        { key: "model", value: "haiku" },
        { key: "system-prompt", value: "be brief" },
        { key: "anything-at-all", value: "" },
      ]),
    ).toEqual({
      status: "ok",
      value: {
        model: "haiku",
        "system-prompt": "be brief",
        "anything-at-all": "",
      },
    });
  });

  it("drops a row carrying neither a key nor a value", () => {
    expect(parseAgentValues([{ key: "", value: "" }])).toEqual({
      status: "ok",
      value: {},
    });
  });

  it("refuses a value with no key", () => {
    expect(parseAgentValues([{ key: "", value: "haiku" }])).toEqual({
      status: "error",
      message: "Every value needs a key.",
    });
  });

  it("names the key whose characters are refused, and takes the same key cased down", () => {
    // The control: `model` is accepted, so what refuses `Model` is the
    // character rule rather than anything else about the row.
    expect(parseAgentValues([{ key: "model", value: "a" }]).status).toBe("ok");
    expect(parseAgentValues([{ key: "Model", value: "a" }])).toEqual({
      status: "error",
      message:
        'The key "Model" holds something other than lowercase letters, digits and hyphens.',
    });
  });

  it("refuses a key past 64 characters and takes one at 64", () => {
    expect(parseAgentValues([{ key: "a".repeat(64), value: "" }]).status).toBe(
      "ok",
    );
    expect(parseAgentValues([{ key: "a".repeat(65), value: "" }])).toEqual({
      status: "error",
      message: "A key holds at most 64 characters; one here holds 65.",
    });
  });

  it("refuses a repeated key rather than silently keeping one value", () => {
    expect(
      parseAgentValues([
        { key: "model", value: "haiku" },
        { key: "model", value: "sonnet" },
      ]),
    ).toEqual({
      status: "error",
      message:
        'The key "model" is used twice; a definition holds one value per key.',
    });
  });

  it("treats a key naming something on Object.prototype as an ordinary key", () => {
    expect(parseAgentValues([{ key: "constructor", value: "haiku" }])).toEqual({
      status: "ok",
      value: { constructor: "haiku" },
    });
  });

  it("measures a value in bytes, not characters", () => {
    // Four bytes per emoji: 1024 of them are the whole allowance, and
    // 1025 characters of ASCII are nowhere near it.
    expect(valueBytes("😀")).toBe(4);
    expect(
      parseAgentValues([{ key: "a", value: "x".repeat(1025) }]).status,
    ).toBe("ok");
    expect(
      parseAgentValues([{ key: "a", value: "😀".repeat(1024) }]).status,
    ).toBe("ok");
    expect(parseAgentValues([{ key: "a", value: "😀".repeat(1025) }])).toEqual({
      status: "error",
      message: 'The value under "a" is 4100 bytes; a value holds at most 4096.',
    });
  });

  it("refuses more entries than a definition carries, and takes that many", () => {
    expect(parseAgentValues(rows(AGENT_VALUES_MAX_ENTRIES)).status).toBe("ok");
    expect(parseAgentValues(rows(AGENT_VALUES_MAX_ENTRIES + 1))).toEqual({
      status: "error",
      message: "A definition carries at most 32 values; this one carries 33.",
    });
  });

  it("publishes the bounds the API states", () => {
    expect(AGENT_VALUES_MAX_ENTRIES).toBe(32);
    expect(AGENT_VALUE_MAX_BYTES).toBe(4096);
  });
});
