import { describe, expect, it } from "vitest";

import { diffLines, diffStat } from "@/lib/line-diff";

describe("diffLines", () => {
  it("keeps the common lines and marks a replaced one removed, then added", () => {
    const lines = diffLines("a\nb\nc", "a\nB\nc");

    expect(lines).toEqual([
      { kind: "same", text: "a", before: 1, after: 1 },
      { kind: "removed", text: "b", before: 2 },
      { kind: "added", text: "B", after: 2 },
      { kind: "same", text: "c", before: 3, after: 3 },
    ]);
    expect(diffStat(lines)).toEqual({ added: 1, removed: 1 });
  });

  it("numbers each side by its own lines across an insertion", () => {
    expect(diffLines("a\nc", "a\nb\nc")).toEqual([
      { kind: "same", text: "a", before: 1, after: 1 },
      { kind: "added", text: "b", after: 2 },
      { kind: "same", text: "c", before: 2, after: 3 },
    ]);
  });

  it("finds no change in equal texts, and every change in disjoint ones", () => {
    expect(diffStat(diffLines("x\ny", "x\ny"))).toEqual({
      added: 0,
      removed: 0,
    });
    // The control: nothing shared, so every line on each side differs.
    expect(diffStat(diffLines("x\ny", "p\nq\nr"))).toEqual({
      added: 3,
      removed: 2,
    });
  });
});
