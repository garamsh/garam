/**
 * The bound an agent's name carries, and the parse that holds it,
 * which the rename form and its Server Action both apply (ADR-0119).
 */

import { refuse, type Parsed } from "@garam/platform-sdk-ts/parsed";

/**
 * How long an agent's name is, in characters, once surrounding space
 * is trimmed (`platform/api/openapi.yaml` §AgentPatch).
 */
export const AGENT_NAME_MAX_LENGTH = 100;

/**
 * A name, trimmed as garam trims it, or the refusal. Nothing left once
 * trimmed is no name, and the agent is shown by its identifier.
 */
export function parseAgentName(name: string): Parsed<string | null> {
  const trimmed = name.trim();
  if (trimmed === "") return { status: "ok", value: null };
  if (Array.from(trimmed).length > AGENT_NAME_MAX_LENGTH) {
    return refuse(`A name holds at most ${AGENT_NAME_MAX_LENGTH} characters.`);
  }
  return { status: "ok", value: trimmed };
}
