"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";
import type { components } from "@garam/platform-client/api";

import { PolicyParser } from "./policies";
import { checkOrgWideScope } from "./policy-scope";

/**
 * What a make-default control has to show: nothing yet, a refusal, or
 * that the policy is active and the confirmation is missing — because
 * it has no attachment, or because its attachments are not listed.
 */
export type RollbackPolicyState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "confirm-org-wide" }
  | { status: "confirm-unknown-scope" };

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
 * attachment evaluates it for the whole organization, and one whose
 * attachments are not listed may, so the change waits until the form
 * confirms that.
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
  if (scope.status !== "proceed") {
    revalidatePath(`/console/orgs/${org}/permissions`, "layout");
    return scope;
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
    return refusal(error);
  }

  revalidatePath(`/console/orgs/${org}/permissions`, "layout");
  return { status: "idle" };
}
