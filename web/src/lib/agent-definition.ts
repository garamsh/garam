/**
 * The bound an agent's name carries, and the parse that holds it,
 * which the rename action applies at the boundary. garam accepts no
 * definition's values (ADR-0090), so nothing here parses one.
 */

import { refuse, type Parsed } from "@/lib/parsed";

/**
 * How long the name of an agent or a template is, in characters, once
 * surrounding space is trimmed (`api/openapi.yaml` §AgentName).
 */
export const AGENT_NAME_MAX_LENGTH = 100;

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
