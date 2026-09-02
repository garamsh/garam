"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
} from "@/lib/garam";
import { parseOperatorIdentifier } from "@/lib/operator";
import type { components } from "@/types/api";

import { slugFromGrn } from "../grn";
import { OperatorParser, type Operator } from "./orgs";

/** What the register form has to show: nothing yet, a refusal, or the
 * operator the organization now holds a record of. */
export type RegisterOperatorState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "registered"; operator: string };

const Schema = z.object({
  org: z.string().min(1),
  identifier: z.string(),
});

/**
 * Registers an operator of the organization (ADR-0045). It hands the
 * operator nothing: what it authenticates with is a certificate garam's
 * own command mints, and this write does not reach it.
 */
export async function registerOperator(
  _state: RegisterOperatorState,
  formData: FormData,
): Promise<RegisterOperatorState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    identifier: formData.get("identifier"),
  });
  if (!parsed.success) {
    return { status: "error", message: "The form did not arrive complete." };
  }

  const identifier = parseOperatorIdentifier(parsed.data.identifier);
  if (identifier.status === "error") return identifier;

  const org = parsed.data.org;
  let operator: Operator;
  try {
    operator = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/operators`,
      OperatorParser,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          identifier: identifier.value,
        } satisfies components["schemas"]["OperatorRegistration"]),
      },
    );
  } catch (error) {
    // An unreachable API is a state the form renders, not a throw: the
    // boundary would replace the form and the identifier typed into it.
    if (
      error instanceof GaramApiError ||
      error instanceof GaramUnreachableError
    ) {
      return { status: "error", message: error.message };
    }
    throw error;
  }

  revalidatePath(`/console/orgs/${org}/operators`);
  return { status: "registered", operator: slugFromGrn(operator.grn) };
}
