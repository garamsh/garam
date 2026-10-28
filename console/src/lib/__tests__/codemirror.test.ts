import { json } from "@codemirror/lang-json";
import { EditorState } from "@codemirror/state";
import { describe, expect, it } from "vitest";

import { pointerRange } from "@/lib/codemirror";

const TEXT = JSON.stringify(
  {
    Version: "2026-07-25",
    Statement: [
      { Effect: "Deny", Action: ["a:b"], Resource: ["*"] },
      { Effect: "Maybe", Resource: ["*"] },
    ],
  },
  null,
  2,
);

function marked(pointer: string): string {
  const state = EditorState.create({ doc: TEXT, extensions: [json()] });
  const { from, to } = pointerRange(state, pointer);
  return state.sliceDoc(from, to);
}

describe("pointerRange", () => {
  it("marks the value a pointer names", () => {
    expect(marked("/Statement/1/Effect")).toBe('"Maybe"');
    expect(marked("/Statement/0/Action/0")).toBe('"a:b"');
    // The control: the same key in the other statement is another
    // place.
    expect(marked("/Statement/0/Effect")).toBe('"Deny"');
  });

  it("marks a container at its key", () => {
    expect(marked("/Statement")).toBe('"Statement"');
  });

  it("marks where a missing key would be: the key of what should hold it", () => {
    // Statement 2 has no Action; its element has no key of its own, so
    // its opening line is marked.
    expect(marked("/Statement/1/Action")).toBe("{");
  });

  it("marks the whole document for the empty pointer or text it cannot place", () => {
    expect(marked("")).toBe("{");
    const state = EditorState.create({ doc: TEXT, extensions: [json()] });
    expect(pointerRange(state, "not a pointer")).toEqual({
      from: 0,
      to: TEXT.length,
    });
  });

  it("marks a key garam names canonically where the document cased it otherwise", () => {
    const text = '{\n  "statement": [\n    { "effect": "Maybe" }\n  ]\n}';
    const state = EditorState.create({ doc: text, extensions: [json()] });
    const { from, to } = pointerRange(state, "/Statement/0/Effect");
    expect(state.sliceDoc(from, to)).toBe('"Maybe"');
  });
});
