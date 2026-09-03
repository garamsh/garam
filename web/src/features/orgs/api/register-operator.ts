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
import { RegisteredOperatorParser, type RegisteredOperator } from "./orgs";

/** What the register form has to show: nothing yet, a refusal, or the
 * operator the organization now holds a record of together with the
 * one-time token that enrols it.
 *
 * The token is carried in this state because the answer that created it
 * is the only place it exists: garam stores its hash, so nothing can
 * fetch it again once this state is replaced (ADR-0063). */
export type RegisterOperatorState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | {
      status: "registered";
      operator: string;
      token: string;
      expiresAt: string;
    };

const Schema = z.object({
  org: z.string().min(1),
  identifier: z.string(),
});

/**
 * Registers an operator of the organization (ADR-0045) and returns the
 * one-time token it enrols with (ADR-0063).
 *
 * No private key passes through here. The operator generates its own
 * key pair in its own cluster and presents this token with a
 * certificate signing request; what garam signs is a public key, and
 * what this action holds is a token that is worthless once spent.
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
  let registered: RegisteredOperator;
  try {
    registered = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/operators`,
      RegisteredOperatorParser,
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
  return {
    status: "registered",
    operator: slugFromGrn(registered.operator.grn),
    token: registered.enrollment.token,
    expiresAt: registered.enrollment.expiresAt,
  };
}
