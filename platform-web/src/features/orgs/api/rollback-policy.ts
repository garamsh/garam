"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated } from "@/lib/garam";
import type { components } from "@garam/platform-sdk-ts/api";

import { DEFAULT_MOVED, policyRefusal } from "./policy-refusal";

/** What a make-default control shows: nothing yet, or a refusal. */
export type RollbackPolicyState =
  { status: "idle" } | { status: "error"; message: string };

const Schema = z.object({
  org: z.string().min(1),
  policy: z.string().min(1),
  version: z.coerce.number().int().min(1),
});

/**
 * Makes one stored version the policy's default, the one it decides by
 * while active. The operation is garam's rollback, which accepts any
 * stored version, older or newer (ADR-0169).
 *
 * A 409 says the default moved since the page was read, and is worded
 * so; 504 and 503 say the policy service is unavailable.
 */
export async function rollbackPolicy(
  _state: RollbackPolicyState,
  formData: FormData,
): Promise<RollbackPolicyState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    policy: formData.get("policy"),
    version: formData.get("version"),
  });
  if (!parsed.success) {
    return { status: "error", message: "The form did not arrive complete." };
  }

  const { org, policy, version } = parsed.data;
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/iam-policies/${encodeURIComponent(policy)}/rollback`,
      z.unknown(),
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          version,
        } satisfies components["schemas"]["PolicyRollback"]),
      },
    );
  } catch (error) {
    return policyRefusal(error, DEFAULT_MOVED);
  }

  revalidatePath(`/console/orgs/${org}/permissions`, "layout");
  return { status: "idle" };
}
