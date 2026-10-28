/**
 * The bounds a local account's username and password carry, the parses
 * that hold them, and the one sentence a refused credential is
 * answered with. The Server Action applies the parses at the boundary,
 * so nothing outside a bound reaches the API; the register form states
 * the same bounds as help text, so a person meets them before
 * submitting rather than after.
 *
 * The numbers are the ones `platform/api/openapi.yaml` publishes under
 * `PasswordRegistration`, which is the only schema that states them:
 * the password's floor and ceiling, and the username's character rule.
 *
 * Signing in parses neither field. A wrong password and a username no
 * account holds are one answer (ADR-0041), and a form that refused a
 * mis-shaped username on its own would answer a third way and say
 * which of the two it was holding.
 */

import { refuse, type Parsed } from "./parsed";

/**
 * How long a username is, in characters. The schema states no number:
 * it states that the username becomes the account's GRN identifier
 * under the reserved `local-` prefix, and an identifier holds at most
 * 64 characters (`docs/architecture/grn-model.md` §identifier). Six of
 * them are spent before the username starts.
 */
export const USERNAME_MAX_LENGTH = 58;

/** How many characters a password holds at least. */
export const PASSWORD_MIN_LENGTH = 15;

/** How many bytes a password holds at most. */
export const PASSWORD_MAX_BYTES = 128;

/**
 * What the console says when a sign-in is refused, whichever of the two
 * it was: a password that does not verify, or a username no account
 * holds. The API refuses them identically and the console does not
 * rest on that — it holds one sentence of its own, so no answer from
 * the API can make the interface name which case it had (ADR-0041).
 */
export const PASSWORD_REFUSED =
  "That username and password do not match an account.";

const USERNAME = /^[a-z0-9-]+$/;

/**
 * Reads the username a new account is to have. The character rule is
 * the GRN segment rule the identifier is built under, so a username
 * carrying anything else would name a different resource or none.
 */
export function parseUsername(input: string): Parsed<string> {
  const username = input.trim();
  if (username === "") {
    return refuse("Choose the username the account is to have.");
  }
  if (username.length > USERNAME_MAX_LENGTH) {
    return refuse(
      `A username holds at most ${USERNAME_MAX_LENGTH} characters; this one holds ${username.length}.`,
    );
  }
  if (!USERNAME.test(username)) {
    return refuse(
      "A username holds only lowercase letters, digits and hyphens.",
    );
  }
  return { status: "ok", value: username };
}

/**
 * Reads a password for a new account. Characters for the floor and
 * bytes for the ceiling, as the schema states them; a character is a
 * code point, so a password written in astral characters is counted as
 * a person typed it rather than as UTF-16 units.
 *
 * Nothing is trimmed — a space a person chose is part of the password —
 * and neither refusal carries what was typed or a measurement of it.
 */
export function parsePassword(input: string): Parsed<string> {
  if ([...input].length < PASSWORD_MIN_LENGTH) {
    return refuse(
      `A password holds at least ${PASSWORD_MIN_LENGTH} characters.`,
    );
  }
  if (new TextEncoder().encode(input).length > PASSWORD_MAX_BYTES) {
    return refuse(`A password holds at most ${PASSWORD_MAX_BYTES} bytes.`);
  }
  return { status: "ok", value: input };
}
