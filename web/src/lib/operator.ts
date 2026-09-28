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
 *
 * The name and description beside it are what the organization says
 * about the operator, and their bounds are `api/openapi.yaml`
 * §OperatorName and §OperatorDescription (ADR-0074). Registering and
 * editing both carry them.
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

/** How long an operator's name is, in characters. */
export const OPERATOR_NAME_MAX_LENGTH = 100;
/** How long an operator's description is, in characters. */
export const OPERATOR_DESCRIPTION_MAX_LENGTH = 1000;

/**
 * Characters as the API counts them: code points, so a character
 * outside the Basic Multilingual Plane is one and not two.
 */
function characters(input: string): number {
  return [...input].length;
}

/** Reads what the organization calls the operator, trimmed. */
export function parseOperatorName(input: string): Parsed<string> {
  const name = input.trim();
  if (name.includes("\0")) {
    return refuse("An operator's name cannot hold a NUL character.");
  }
  if (name === "") {
    return refuse("Give the operator a name.");
  }
  if (characters(name) > OPERATOR_NAME_MAX_LENGTH) {
    return refuse(
      `An operator's name holds at most ${OPERATOR_NAME_MAX_LENGTH} characters; this one holds ${characters(name)}.`,
    );
  }
  return { status: "ok", value: name };
}

/**
 * Reads what the organization says the operator is for, trimmed. Empty
 * is a description of its own: none.
 */
export function parseOperatorDescription(input: string): Parsed<string> {
  const description = input.trim();
  if (description.includes("\0")) {
    return refuse("A description cannot hold a NUL character.");
  }
  if (characters(description) > OPERATOR_DESCRIPTION_MAX_LENGTH) {
    return refuse(
      `A description holds at most ${OPERATOR_DESCRIPTION_MAX_LENGTH} characters; this one holds ${characters(description)}.`,
    );
  }
  return { status: "ok", value: description };
}
