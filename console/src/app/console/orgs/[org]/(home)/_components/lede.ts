/** A thing the lede names, linked to its page. */
export type LedeLink = { text: string; href: string };

/** The lede, in order: plain text and linked names. */
export type LedePart = string | LedeLink;

/** How many names one sentence lists before it counts the rest. */
const NAMED = 3;

/** "A", "A and B", "A, B and C", "A, B, C and 2 more". */
function names(links: readonly LedeLink[]): LedePart[] {
  const shown = links.slice(0, NAMED);
  const more = links.length - shown.length;
  const tail: LedePart[] = more > 0 ? [`${more} more`] : [];
  const items: LedePart[] = [...shown, ...tail];
  return items.flatMap((item, index) => {
    if (index === 0) return [item];
    return [index === items.length - 1 ? " and " : ", ", item];
  });
}

/**
 * The sentence Home opens with, generated from what it read
 * (ADR-0113). Each part it could not read is left out rather than
 * guessed: `teams` and `attention` are null where their reads failed.
 * Nothing is ever waiting on anyone yet, since garam has no work items.
 */
export function homeLede({
  teams,
  attention,
}: {
  /** The caller's teams. */
  teams: readonly LedeLink[] | null;
  /** What the caller can read that is failing. */
  attention: readonly LedeLink[] | null;
}): LedePart[] {
  const parts: LedePart[] = ["Nothing is waiting on you."];
  if (teams !== null) {
    parts.push(
      ...(teams.length === 0
        ? [" You are in no team here."]
        : [" You are in ", ...names(teams), "."]),
    );
  }
  if (attention !== null) {
    parts.push(
      ...(attention.length === 0
        ? [" Nothing you can read is failing."]
        : attention.length === 1
          ? [" ", attention[0], " needs a look."]
          : [" These need a look: ", ...names(attention), "."]),
    );
  }
  return parts;
}
