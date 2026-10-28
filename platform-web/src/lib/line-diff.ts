/** One line of a diff: kept, added in the new text, or removed from the old. */
export type DiffLine =
  | { kind: "same"; text: string; before: number; after: number }
  | { kind: "added"; text: string; after: number }
  | { kind: "removed"; text: string; before: number };

/**
 * The lines that turn `before` into `after`, by longest common
 * subsequence, with each line's 1-based number on the side it is on.
 * Policy documents run to tens of lines, so the quadratic table is
 * cheap; a removal is listed before the addition that replaces it.
 */
export function diffLines(before: string, after: string): DiffLine[] {
  const old = before.split("\n");
  const next = after.split("\n");
  // common[i][j]: the longest common run of old[i..] and next[j..].
  const common = Array.from({ length: old.length + 1 }, () =>
    new Array<number>(next.length + 1).fill(0),
  );
  for (let i = old.length - 1; i >= 0; i--) {
    for (let j = next.length - 1; j >= 0; j--) {
      common[i][j] =
        old[i] === next[j]
          ? common[i + 1][j + 1] + 1
          : Math.max(common[i + 1][j], common[i][j + 1]);
    }
  }

  const lines: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < old.length || j < next.length) {
    if (i < old.length && j < next.length && old[i] === next[j]) {
      lines.push({ kind: "same", text: old[i], before: i + 1, after: j + 1 });
      i++;
      j++;
    } else if (
      i < old.length &&
      (j === next.length || common[i + 1][j] >= common[i][j + 1])
    ) {
      lines.push({ kind: "removed", text: old[i], before: i + 1 });
      i++;
    } else {
      lines.push({ kind: "added", text: next[j], after: j + 1 });
      j++;
    }
  }
  return lines;
}

/** How many lines a diff adds and removes. */
export function diffStat(lines: readonly DiffLine[]): {
  added: number;
  removed: number;
} {
  return {
    added: lines.filter((line) => line.kind === "added").length,
    removed: lines.filter((line) => line.kind === "removed").length,
  };
}
