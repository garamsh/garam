"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
} from "@/lib/garam";
import type { components } from "@/types/api";

import { PolicyParser } from "./policies";
import { checkOrgWideScope } from "./policy-scope";

/**
 * What a make-default control has to show: nothing yet, a refusal, or
 * that the policy is active with no attachment and the
 * organization-wide application was not confirmed.
 */
export type RollbackPolicyState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "confirm-org-wide" };

const Schema = z.object({
  org: z.string().min(1),
  policy: z.string().min(1),
  version: z.coerce.number().int().min(1),
  orgWide: z.literal("confirmed").nullable(),
});

/**
 * Makes one stored version the policy's default — the one it evaluates
 * while active. The operation is garam's rollback, which accepts any
 * stored version, older or newer (ADR-0073). An active policy with no
 * attachment evaluates it for the whole organization, so the change
 * waits until the form confirms that.
 */
export async function rollbackPolicy(
  _state: RollbackPolicyState,
  formData: FormData,
): Promise<RollbackPolicyState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    policy: formData.get("policy"),
    version: formData.get("version"),
    orgWide: formData.get("orgWide"),
  });
  if (!parsed.success) {
    return { status: "error", message: "The form did not arrive complete." };
  }

  const { org, policy, version, orgWide } = parsed.data;
  const scope = await checkOrgWideScope(org, policy, {
    confirmed: orgWide === "confirmed",
    onlyIfActive: true,
  });
  if (scope.status === "error") return scope;
  if (scope.status === "confirm") {
    revalidatePath(`/console/orgs/${org}/permissions`, "layout");
    return { status: "confirm-org-wide" };
  }
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/policies/${encodeURIComponent(policy)}/rollback`,
      PolicyParser,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          version,
        } satisfies components["schemas"]["PolicyRollback"]),
      },
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
