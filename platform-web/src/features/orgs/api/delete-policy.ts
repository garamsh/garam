"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";

/**
 * What the delete control has to show: nothing yet, or a refusal. A
 * deleted policy is the list it is sent back to.
 */
export type DeletePolicyState =
  { status: "idle" } | { status: "error"; message: string };

const Schema = z.object({
  org: z.string().min(1),
  policy: z.string().min(1),
});

/**
 * Deletes a policy of the organization. Its versions go with it, so it
 * takes part in no decision from then on (ADR-0169). A policy the
 * policy service will not delete answers 409, which is returned as it
 * is answered.
 */
export async function deletePolicy(
  _state: DeletePolicyState,
  formData: FormData,
): Promise<DeletePolicyState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    policy: formData.get("policy"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Name the policy to delete." };
  }

  const { org, policy } = parsed.data;
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/iam-policies/${encodeURIComponent(policy)}`,
      z.unknown(),
      { method: "DELETE" },
    );
  } catch (error) {
    return refusal(error);
  }

  revalidatePath(`/console/orgs/${org}/permissions`, "layout");
  redirect(`/console/orgs/${org}/permissions`);
}
