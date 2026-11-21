"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { z } from "zod";

import { garamFetchAuthenticated } from "@/lib/garam";
import type { components } from "@garam/platform-sdk-ts/api";

import { BODY_INCOMPLETE, PolicyBodyForm, wireBody } from "./policy-body-form";
import { policyWriteRefusal, type PolicyWriteError } from "./policy-refusal";

/**
 * What the create form has to show: nothing yet, or a refusal. There is
 * no created state: a created policy is the page it is sent to.
 */
export type CreatePolicyState = { status: "idle" } | PolicyWriteError;

const Schema = z.object({
  org: z.string().min(1),
  name: z.string().trim().min(1),
});

const CreatedParser = z.object({ id: z.string().min(1) });

/**
 * Creates a role link, a guardrail or a permit of the organization and
 * opens the page of the one the policy service stored, a draft whose
 * first version is the one written (ADR-0169). The identifier is the
 * platform's, so none is sent.
 *
 * The service decides whether the caller may and whether the body is
 * one it accepts; its refusal comes back as state, with its own message
 * at the body it refused.
 */
export async function createPolicy(
  _state: CreatePolicyState,
  formData: FormData,
): Promise<CreatePolicyState> {
  const fields = Object.fromEntries(formData);
  const named = Schema.safeParse(fields);
  if (!named.success) {
    return named.error.issues.some((issue) => issue.path[0] === "name")
      ? { status: "error", at: "name", message: "Name the policy." }
      : {
          status: "error",
          at: "form",
          message: "The form did not arrive complete.",
        };
  }
  const body = PolicyBodyForm.safeParse(fields);
  if (!body.success) {
    return {
      status: "error",
      at: "body",
      message:
        fields.kind === "role-link"
          ? BODY_INCOMPLETE
          : "Write the policy's Cedar text.",
    };
  }

  const { org, name } = named.data;
  let created: z.infer<typeof CreatedParser>;
  try {
    created = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/iam-policies`,
      CreatedParser,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          name,
          body: wireBody(body.data),
        } satisfies components["schemas"]["IamPolicyCreate"]),
      },
    );
  } catch (error) {
    // A refusal is a state the form renders, not a throw: the boundary
    // would replace the form and what was typed into it.
    return policyWriteRefusal(
      error,
      body.data.kind === "role-link" ? "create-link" : "create-cedar",
    );
  }

  revalidatePath(`/console/orgs/${org}/permissions`, "layout");
  redirect(`/console/orgs/${org}/permissions/${created.id}`);
}
