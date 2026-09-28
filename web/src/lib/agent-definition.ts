/**
 * The bounds an agent definition's inputs carry, and the parse that
 * holds them. The form applies it before submitting and the Server
 * Action applies it again at the boundary, so a person is refused
 * without a round trip and a caller that skips the form is still
 * refused.
 *
 * The numbers are the ones `api/openapi.yaml` publishes under
 * `AgentValues`; the character rule is the GRN segment rule
 * (`docs/architecture/grn-model.md` §identifier). The operator a
 * definition names carries the same rule and is parsed by
 * `src/lib/operator.ts`, which two forms reach.
 */

import { refuse, type Parsed } from "@/lib/parsed";

/** How many values one definition carries. */
export const AGENT_VALUES_MAX_ENTRIES = 32;

/** How long one value is, in bytes. */
export const AGENT_VALUE_MAX_BYTES = 4096;

/** How long a value key is, in characters. */
export const AGENT_KEY_MAX_LENGTH = 64;

/**
 * How long the name of an agent or a template is, in characters, once
 * surrounding space is trimmed (`api/openapi.yaml` §AgentName).
 */
export const AGENT_NAME_MAX_LENGTH = 100;

/** One row of the values editor, before it is known to be a value. */
export type ValueEntry = { key: string; value: string };

const SEGMENT = /^[a-z0-9-]+$/;

/** The character count garam measures a name by: code points. */
export function characters(text: string): number {
  return Array.from(text).length;
}

/**
 * A name, trimmed as garam trims it, or the refusal. `missing` is the
 * sentence an empty one is refused with, which names what is being
 * named.
 */
export function parseAgentName(
  name: string,
  missing = "Give the agent a name.",
): Parsed<string> {
  const trimmed = name.trim();
  if (trimmed === "") return refuse(missing);
  if (characters(trimmed) > AGENT_NAME_MAX_LENGTH) {
    return refuse(`A name holds at most ${AGENT_NAME_MAX_LENGTH} characters.`);
  }
  return { status: "ok", value: trimmed };
}

/** The UTF-8 byte length a value is measured by. */
export function valueBytes(value: string): number {
  return new TextEncoder().encode(value).length;
}

/**
 * Reads the values a definition carries out of the editor's rows. A row
 * with neither a key nor a value is dropped: the editor opens with one,
 * and a definition carrying no values is a definition.
 *
 * garam interprets no key, so nothing here reads one — the keys are
 * checked for shape and for being distinct, and the strings under them
 * are checked for size and passed on.
 */
export function parseAgentValues(
  entries: readonly ValueEntry[],
): Parsed<Record<string, string>> {
  const rows = entries.filter((row) => row.key !== "" || row.value !== "");
  if (rows.length > AGENT_VALUES_MAX_ENTRIES) {
    return refuse(
      `A definition carries at most ${AGENT_VALUES_MAX_ENTRIES} values; this one carries ${rows.length}.`,
    );
  }
  // A Map, so a key that names something on Object.prototype is a key
  // like any other rather than a duplicate of nothing.
  const values = new Map<string, string>();
  for (const { key, value } of rows) {
    if (key === "") {
      return refuse("Every value needs a key.");
    }
    if (key.length > AGENT_KEY_MAX_LENGTH) {
      return refuse(
        `A key holds at most ${AGENT_KEY_MAX_LENGTH} characters; one here holds ${key.length}.`,
      );
    }
    if (!SEGMENT.test(key)) {
      return refuse(
        `The key "${key}" holds something other than lowercase letters, digits and hyphens.`,
      );
    }
    if (values.has(key)) {
      return refuse(
        `The key "${key}" is used twice; a definition holds one value per key.`,
      );
    }
    const bytes = valueBytes(value);
    if (bytes > AGENT_VALUE_MAX_BYTES) {
      return refuse(
        `The value under "${key}" is ${bytes} bytes; a value holds at most ${AGENT_VALUE_MAX_BYTES}.`,
      );
    }
    values.set(key, value);
  }
  return { status: "ok", value: Object.fromEntries(values) };
}
