/**
 * The rule an operator identifier follows, and the parse that holds it.
 * A form applies it before submitting and a Server Action applies it
 * again at the boundary, so a person is refused without a round trip
 * and a caller that skips the form is still refused.
 *
 * Two forms carry an operator identifier — registering one, and naming
 * one in an agent definition — which is why the rule lives here rather
 * than beside either.
 *
 * The character rule is the GRN segment rule
 * (`docs/architecture/grn-model.md` §identifier). What garam refuses on
 * top of it is not shape but absence: an identifier no operator is
 * registered under is refused by the API, which names it (ADR-0045).
 */

import { refuse, type Parsed } from "@/lib/parsed";

/** How long an operator identifier is, in characters. */
export const OPERATOR_IDENTIFIER_MAX_LENGTH = 64;

const SEGMENT = /^[a-z0-9-]+$/;

/**
 * Reads an operator identifier: the GRN's last segment, which is what
 * the console addresses an operator by (ADR-0011). A segment carrying
 * a colon would name a different resource once it is built into a GRN,
 * which is what the character rule keeps out.
 */
export function parseOperatorIdentifier(input: string): Parsed<string> {
  const identifier = input.trim();
  if (identifier === "") {
    return refuse("Name the operator.");
  }
  if (identifier.length > OPERATOR_IDENTIFIER_MAX_LENGTH) {
    return refuse(
      `An operator identifier holds at most ${OPERATOR_IDENTIFIER_MAX_LENGTH} characters; this one holds ${identifier.length}.`,
    );
  }
  if (!SEGMENT.test(identifier)) {
    return refuse(
      "An operator identifier holds only lowercase letters, digits and hyphens.",
    );
  }
  return { status: "ok", value: identifier };
}
