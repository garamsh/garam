"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { z } from "zod";

import { parseAgentName, parseAgentValues } from "@/lib/agent-definition";
import { parseOperatorIdentifier } from "@/lib/operator";
import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
} from "@/lib/garam";
import type { components } from "@/types/api";

import { operatorGrn, slugFromGrn } from "../grn";
import { AgentDefinitionParser, type AgentDefinition } from "./orgs";

/**
 * What the define form has to show: nothing yet, or a refusal. A
 * defined agent is the page it is sent to.
 */
export type DefineAgentState =
  { status: "idle" } | { status: "error"; message: string };

const Schema = z.object({
  org: z.string().min(1),
  name: z.string(),
  operator: z.string(),
  entries: z.array(z.object({ key: z.string(), value: z.string() })),
});

/**
 * Records what an agent of the organization should be and opens the
 * page of the agent it minted. It creates no agent: an operator reads
 * the definition and makes one (ADR-0026). Values a template supplied
 * arrive here as the form's own rows, so nothing links the definition
 * to that template (ADR-0075).
 */
export async function defineAgent(
  _state: DefineAgentState,
  formData: FormData,
): Promise<DefineAgentState> {
  // The editor submits one `key` and one `value` field per row, so a
  // submission whose two lists disagree leaves a row with a half
  // missing and the parse below refuses it.
  const keyFields = formData.getAll("key");
  const valueFields = formData.getAll("value");
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    name: formData.get("name"),
    operator: formData.get("operator"),
    entries: Array.from(
      { length: Math.max(keyFields.length, valueFields.length) },
      (_, index) => ({ key: keyFields[index], value: valueFields[index] }),
    ),
  });
  if (!parsed.success) {
    return { status: "error", message: "The form did not arrive complete." };
  }

  const name = parseAgentName(parsed.data.name);
  if (name.status === "error") return name;
  const operator = parseOperatorIdentifier(parsed.data.operator);
  if (operator.status === "error") return operator;
  const values = parseAgentValues(parsed.data.entries);
  if (values.status === "error") return values;

  const org = parsed.data.org;
  let definition: AgentDefinition;
  try {
    definition = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/agent-definitions`,
      AgentDefinitionParser,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          name: name.value,
          operatorGrn: operatorGrn(org, operator.value),
          values: values.value,
        } satisfies components["schemas"]["AgentDefinitionCreate"]),
      },
    );
  } catch (error) {
    // An unreachable API is a state the form renders, not a throw: the
    // boundary would replace the form and everything typed into it,
    // and a value may be four kilobytes of prompt.
    if (
      error instanceof GaramApiError ||
      error instanceof GaramUnreachableError
    ) {
      return { status: "error", message: error.message };
    }
    throw error;
  }

  revalidatePath(`/console/orgs/${org}/agents`, "layout");
  redirect(`/console/orgs/${org}/agents/${slugFromGrn(definition.agentGrn)}`);
}
