"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
} from "@/lib/garam";

import { PolicyParser } from "./policies";
import { checkOrgWideScope } from "./policy-scope";

/**
 * What an activation control has to show: nothing yet, a refusal, or
 * that the policy has no attachment and the organization-wide
 * application was not confirmed.
 */
export type ActivatePolicyState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "confirm-org-wide" };

const Schema = z.object({
  org: z.string().min(1),
  policy: z.string().min(1),
  orgWide: z.literal("confirmed").nullable(),
});

/**
 * Activates a draft or archived policy, so its default version takes
 * part in every check its scope reaches. With no attachment that scope
 * is the whole organization, and the activation waits until the form
 * confirms it.
 */
export async function activatePolicy(
  _state: ActivatePolicyState,
  formData: FormData,
): Promise<ActivatePolicyState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    policy: formData.get("policy"),
    orgWide: formData.get("orgWide"),
  });
  if (!parsed.success) {
    return { status: "error", message: "The form did not arrive complete." };
  }

  const { org, policy, orgWide } = parsed.data;
  const scope = await checkOrgWideScope(org, policy, {
    confirmed: orgWide === "confirmed",
    onlyIfActive: false,
  });
  if (scope.status === "error") return scope;
  if (scope.status === "confirm") {
    revalidatePath(`/console/orgs/${org}/permissions`, "layout");
    return { status: "confirm-org-wide" };
  }
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/policies/${encodeURIComponent(policy)}/activate`,
      PolicyParser,
      { method: "POST" },
    );
  } catch (error) {
    if (
      error instanceof GaramApiError ||
      error instanceof GaramUnreachableError
    ) {
      return { status: "error", message: error.message };
    }
    throw error;
  }

  revalidatePath(`/console/orgs/${org}/permissions`, "layout");
  return { status: "idle" };
}
