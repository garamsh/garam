"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import {
  parseAgentValues,
  parseOperatorIdentifier,
  type ValueEntry,
} from "@/lib/agent-definition";
import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
} from "@/lib/garam";
import type { components } from "@/types/api";

import { operatorGrn, slugFromGrn } from "../grn";
import type { AgentDefinition } from "./orgs";

/**
 * What the define form has to show: nothing yet, a refusal, or the
 * identifier garam minted for the agent the definition describes.
 */
export type DefineAgentState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "defined"; agent: string };

// The editor submits one `key` and one `value` field per row, so the
// two arrays pair up by position.
const Schema = z.object({
  org: z.string().min(1),
  operator: z.string(),
  keys: z.array(z.string()),
  values: z.array(z.string()),
});

/**
 * Records what an agent of the organization should be and answers with
 * the agent it minted. It creates no agent: an operator reads the
 * definition and makes one (ADR-0026).
 */
export async function defineAgent(
  _state: DefineAgentState,
  formData: FormData,
): Promise<DefineAgentState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    operator: formData.get("operator"),
    keys: formData.getAll("key"),
    values: formData.getAll("value"),
  });
  if (
    !parsed.success ||
    parsed.data.keys.length !== parsed.data.values.length
  ) {
    return { status: "error", message: "The form did not arrive complete." };
  }

  const operator = parseOperatorIdentifier(parsed.data.operator);
  if (operator.status === "error") return operator;
  const entries: ValueEntry[] = parsed.data.keys.map((key, index) => ({
    key,
    value: parsed.data.values[index],
  }));
  const values = parseAgentValues(entries);
  if (values.status === "error") return values;

  const org = parsed.data.org;
  let definition: AgentDefinition;
  try {
    definition = await garamFetchAuthenticated<AgentDefinition>(
      `/orgs/${encodeURIComponent(org)}/agent-definitions`,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
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

  revalidatePath(`/console/orgs/${org}/agents`);
  return { status: "defined", agent: slugFromGrn(definition.agentGrn) };
}
