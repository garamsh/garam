"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";
import {
  parseOperatorDescription,
  parseOperatorIdentifier,
  parseOperatorName,
} from "@/lib/operator";
import type { components } from "@/types/api";

import { slugFromGrn } from "../grn";
import { RegisteredOperatorParser, type RegisteredOperator } from "./orgs";

/** What the register form has to show: nothing yet, a refusal, or the
 * operator the organization now holds a record of together with the
 * one-time token that enrols it and the credential it exchanges for
 * tokens.
 *
 * The token and the credential are carried in this state because the
 * answer that created them is the only place they exist: garam stores
 * their hashes, so nothing can fetch either again once this state is
 * replaced (ADR-0064, ADR-0100). */
export type RegisterOperatorState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | {
      status: "registered";
      operator: string;
      name: string;
      token: string;
      expiresAt: string;
      credential: string;
    };

const Schema = z.object({
  org: z.string().min(1),
  identifier: z.string(),
  name: z.string(),
  description: z.string(),
});

/**
 * Registers an operator of the organization (ADR-0045) and returns the
 * one-time token it enrols with (ADR-0064) and the credential it
 * exchanges for tokens (ADR-0100).
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
  return {
    status: "registered",
    operator: slugFromGrn(registered.operator.grn),
    name: registered.operator.name,
    token: registered.enrollment.token,
    expiresAt: registered.enrollment.expiresAt,
    credential: registered.credential.credential,
  };
}
