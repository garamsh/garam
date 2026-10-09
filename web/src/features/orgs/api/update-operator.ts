"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";
import { parseOperatorDescription, parseOperatorName } from "@/lib/operator";
import type { components } from "@/types/api";

import { OperatorParser } from "./orgs";

/**
 * What the details form has to show: nothing yet, a refusal, or the
 * details saved.
 */
export type UpdateOperatorState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "saved"; name: string };

const Schema = z.object({
  org: z.string().min(1),
  operator: z.string().min(1),
  name: z.string(),
  description: z.string(),
});

/**
 * Changes what the organization calls an operator and what it says the
 * operator is for (ADR-0074). The identifier is in the path and not the
 * body, so the operator's GRN, its certificate lineage and the
 * definitions naming it stay as they were.
 */
export async function updateOperator(
  _state: UpdateOperatorState,
  formData: FormData,
): Promise<UpdateOperatorState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    operator: formData.get("operator"),
    name: formData.get("name"),
    description: formData.get("description"),
  });
  if (!parsed.success) {
    return { status: "error", message: "The form did not arrive complete." };
  }

  const name = parseOperatorName(parsed.data.name);
  if (name.status === "error") return name;
  const description = parseOperatorDescription(parsed.data.description);
  if (description.status === "error") return description;

  const { org, operator } = parsed.data;
  let saved: string;
  try {
    ({ name: saved } = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/operators/${encodeURIComponent(operator)}`,
      OperatorParser,
      {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          name: name.value,
          description: description.value,
        } satisfies components["schemas"]["OperatorPatch"]),
      },
    ));
  } catch (error) {
    return refusal(error);
  }

  revalidatePath(`/console/orgs/${org}/operators`, "layout");
  revalidatePath(`/console/orgs/${org}/agents`);
  return { status: "saved", name: saved };
}
