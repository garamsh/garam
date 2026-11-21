"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated } from "@/lib/garam";
import type { components } from "@garam/platform-sdk-ts/api";

import { PolicyBodyForm, wireBody } from "./policy-body-form";
import { policyWriteRefusal, type PolicyWriteError } from "./policy-refusal";

/**
 * What the version editor has to show: nothing yet, the version the
 * policy service stored, or a refusal. A stored version is not the
 * default, so the editor stays where it is and says so.
 */
export type CreatePolicyVersionState =
  { status: "idle" } | { status: "stored"; version: number } | PolicyWriteError;

const Schema = z.object({
  org: z.string().min(1),
  policy: z.string().min(1),
});

const StoredParser = z.object({ version: z.number().int().min(1) });

/**
 * Appends a version to a guardrail or a permit. The policy service
 * stores it as a version and not as the default, so what decides is
 * unchanged until the policy's default moves (ADR-0169); a role link
 * takes no version, since a changed link is a new one.
 *
 * The service decides whether the caller may and whether the text is
 * one it accepts; its refusal comes back as state, with its own message
 * at the editor.
 */
export async function createPolicyVersion(
  _state: CreatePolicyVersionState,
  formData: FormData,
): Promise<CreatePolicyVersionState> {
  const fields = Object.fromEntries(formData);
  const target = Schema.safeParse(fields);
  if (!target.success) {
    return {
      status: "error",
      at: "form",
      message: "The form did not arrive complete.",
    };
  }
  const body = PolicyBodyForm.safeParse(fields);
  if (!body.success || body.data.kind === "role-link") {
    return {
      status: "error",
      at: "body",
      message: "Write the policy's Cedar text.",
    };
  }

  const { org, policy } = target.data;
  let stored: z.infer<typeof StoredParser>;
  try {
    stored = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/iam-policies/${encodeURIComponent(policy)}/versions`,
      StoredParser,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          body: wireBody(body.data),
        } satisfies components["schemas"]["IamPolicyVersionCreate"]),
      },
    );
  } catch (error) {
    return policyWriteRefusal(error, "version");
  }

  revalidatePath(`/console/orgs/${org}/permissions`, "layout");
  return { status: "stored", version: stored.version };
}
