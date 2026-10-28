/**
 * What a parse of a person's input answers with: the value, or the
 * sentence shown in place of it. A form applies the parse before
 * submitting and the Server Action applies it again at the boundary,
 * so both sides carry one shape rather than two.
 */

/** A parsed input, or the sentence to show in place of it. */
export type Parsed<T> =
  { status: "ok"; value: T } | { status: "error"; message: string };

/** The refusal a parse answers with. */
export function refuse(message: string): { status: "error"; message: string } {
  return { status: "error", message };
}
