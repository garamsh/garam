import { describe, expect, it } from "vitest";

import { parsePolicyDocument } from "@/lib/policy-document";

describe("parsePolicyDocument", () => {
  it.each([
    ["text that is not JSON", "{ Version:", "The document is not valid JSON."],
    ["a JSON array", "[]", "The document must be a JSON object."],
    ["JSON null", "null", "The document must be a JSON object."],
    ["a JSON string", '"Deny"', "The document must be a JSON object."],
  ])("refuses %s, and takes the object beside it", (_, text, message) => {
    expect(parsePolicyDocument(text)).toEqual({ status: "error", message });
    // The control: an object is taken whole, keys the console does not
    // know included, since the engine is what judges them.
    expect(parsePolicyDocument('{"Version":"2026-07-25","X":1}')).toEqual({
      status: "ok",
      value: { Version: "2026-07-25", X: 1 },
    });
  });
});
