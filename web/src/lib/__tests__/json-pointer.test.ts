import { describe, expect, it } from "vitest";

import { pointerTokens } from "@/lib/json-pointer";

describe("pointerTokens", () => {
  it("splits a pointer and unescapes its tokens", () => {
    expect(pointerTokens("/Statement/1/Effect")).toEqual([
      "Statement",
      "1",
      "Effect",
    ]);
    // RFC 6901: ~1 is a slash and ~0 a tilde, in that order.
    expect(pointerTokens("/a~1b/c~0d/~01")).toEqual(["a/b", "c~d", "~1"]);
  });

  it("reads the empty pointer as the whole document and refuses other text", () => {
    expect(pointerTokens("")).toEqual([]);
    expect(pointerTokens("Statement/1")).toBeNull();
  });
});
