"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";
import {
  parseOperatorDescription,
  parseOperatorIdentifier,
  parseOperatorName,
} from "@/lib/operator";
import type { components } from "@/types/api";

import { slugFromGrn } from "@/lib/grn";
import { OperatorParser, type Operator } from "./orgs";

/** What the register form has to show: nothing yet, or a refusal. A
 * registered operator is sent on to its setup page. */
export type RegisterOperatorState =
  { status: "idle" } | { status: "error"; message: string };

const Schema = z.object({
  org: z.string().min(1),
  identifier: z.string(),
  name: z.string(),
  description: z.string(),
});

/**
 * Registers an operator of the organization (ADR-0045) and opens its
 * setup page, where its key is issued: registering issues nothing, and
 * an operator enters by a key alone (ADR-0102, ADR-0117).
 */
export async function registerOperator(
  _state: RegisterOperatorState,
  formData: FormData,
): Promise<RegisterOperatorState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    identifier: formData.get("identifier"),
    name: formData.get("name"),
    description: formData.get("description"),
  });
  if (!parsed.success) {
    return { status: "error", message: "The form did not arrive complete." };
  }

  const identifier = parseOperatorIdentifier(parsed.data.identifier);
  if (identifier.status === "error") return identifier;
  // A name left empty is the identifier's, which is what the API calls
  // an operator registered without one (ADR-0074).
  const name =
    parsed.data.name.trim() === ""
      ? undefined
      : parseOperatorName(parsed.data.name);
  if (name?.status === "error") return name;
  const description = parseOperatorDescription(parsed.data.description);
  if (description.status === "error") return description;

  const org = parsed.data.org;
  let registered: Operator;
  try {
    registered = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/operators`,
      OperatorParser,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          identifier: identifier.value,
          name: name?.value,
          description: description.value,
        } satisfies components["schemas"]["OperatorRegistration"]),
      },
    );
  } catch (error) {
    // An unreachable API is a state the form renders, not a throw: the
    // boundary would replace the form and the identifier typed into it.
    return refusal(error);
  }

  revalidatePath(`/console/orgs/${org}/operators`, "layout");
  revalidatePath(`/console/orgs/${org}/agents`);
  redirect(
    `/console/orgs/${org}/operators/${slugFromGrn(registered.grn)}/connect`,
  );
}
