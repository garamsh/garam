"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";

/** What an activation control has to show: nothing yet, or a refusal. */
export type ActivatePolicyState =
  { status: "idle" } | { status: "error"; message: string };

const Schema = z.object({
  org: z.string().min(1),
  policy: z.string().min(1),
});

/**
 * Activates a draft policy, so its default version takes part in every
 * decision the policy service makes for the organization (ADR-0169).
 *
 * The policy service decides whether the caller may, so a refusal it
 * gives, or its being unavailable, is returned rather than read as
 * success.
 */
export async function activatePolicy(
  _state: ActivatePolicyState,
  formData: FormData,
): Promise<ActivatePolicyState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    policy: formData.get("policy"),
  });
  if (!parsed.success) {
    return { status: "error", message: "The form did not arrive complete." };
  }

  const { org, policy } = parsed.data;
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/iam-policies/${encodeURIComponent(policy)}/activate`,
      z.unknown(),
      { method: "POST" },
    );
  } catch (error) {
    return refusal(error);
  }

  revalidatePath(`/console/orgs/${org}/permissions`, "layout");
  return { status: "idle" };
}
