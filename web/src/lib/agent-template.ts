/**
 * The bounds a template's own fields carry, and the parse that holds
 * them. The form applies it before submitting and the Server Action
 * applies it again at the boundary. The values a template carries are
 * a definition's values and are parsed by `src/lib/agent-definition.ts`.
 *
 * The numbers are the ones `api/openapi.yaml` publishes under
 * `AgentTemplateWrite`, counted in characters as garam counts them.
 */

import { parseAgentValues, type ValueEntry } from "@/lib/agent-definition";
import { refuse, type Parsed } from "@/lib/parsed";

/** How long a template's name is, in characters. */
export const TEMPLATE_NAME_MAX_LENGTH = 100;

/** How long a template's description is, in characters. */
export const TEMPLATE_DESCRIPTION_MAX_LENGTH = 1000;

/** What a template says, once it is known to be one garam accepts. */
export type TemplateFields = {
  name: string;
  description: string;
  values: Record<string, string>;
};

/** The character count garam measures a name by: code points. */
export function characters(text: string): number {
  return Array.from(text).length;
}

/** A template's name, trimmed as garam trims it, or the refusal. */
export function parseTemplateName(name: string): Parsed<string> {
  const trimmed = name.trim();
  if (trimmed === "") {
    return refuse("Give the template a name.");
  }
  if (characters(trimmed) > TEMPLATE_NAME_MAX_LENGTH) {
    return refuse(
      `A name holds at most ${TEMPLATE_NAME_MAX_LENGTH} characters.`,
    );
  }
  return { status: "ok", value: trimmed };
}

/** A template's description, trimmed as garam trims it, or the refusal. */
export function parseTemplateDescription(description: string): Parsed<string> {
  const trimmed = description.trim();
  if (characters(trimmed) > TEMPLATE_DESCRIPTION_MAX_LENGTH) {
    return refuse(
      `A description holds at most ${TEMPLATE_DESCRIPTION_MAX_LENGTH} characters.`,
    );
  }
  return { status: "ok", value: trimmed };
}

/** Reads a template out of the form's fields. */
export function parseTemplateFields(
  name: string,
  description: string,
  entries: readonly ValueEntry[],
): Parsed<TemplateFields> {
  const parsedName = parseTemplateName(name);
  if (parsedName.status === "error") return parsedName;
  const parsedDescription = parseTemplateDescription(description);
  if (parsedDescription.status === "error") return parsedDescription;
  const values = parseAgentValues(entries);
  if (values.status === "error") return values;
  return {
    status: "ok",
    value: {
      name: parsedName.value,
      description: parsedDescription.value,
      values: values.value,
    },
  };
}
