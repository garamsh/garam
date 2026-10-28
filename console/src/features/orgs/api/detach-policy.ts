"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { GaramApiError, garamFetchAuthenticated, refusal } from "@/lib/garam";

import { parseGrn } from "@/lib/grn";

/**
 * What a detach control has to show: nothing yet, a refusal, or
 * garam's refusal to remove an active policy's last attachment.
 */
export type DetachPolicyState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "last-attachment" };

const Schema = z.object({
  org: z.string().min(1),
  policy: z.string().min(1),
  subject: z.string().refine((value) => parseGrn(value) !== null),
});

/**
 * Removes one attachment of a policy. garam refuses the last one of an
 * active policy, which would otherwise apply to the whole organization
 * (ADR-0031); that refusal is its own state so the control can name
 * the ways forward.
 */
export async function detachPolicy(
  _state: DetachPolicyState,
  formData: FormData,
): Promise<DetachPolicyState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    policy: formData.get("policy"),
    subject: formData.get("subject"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Name the attachment to remove." };
  }

  const { org, policy, subject } = parsed.data;
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/policies/${encodeURIComponent(policy)}/attachments/${encodeURIComponent(subject)}`,
      z.unknown(),
      { method: "DELETE" },
    );
  } catch (error) {
    // The operation answers 409 for this refusal alone
    // (`api/openapi.yaml` detachPolicy).
    if (error instanceof GaramApiError && error.status === 409) {
      return { status: "last-attachment" };
    }
    return refusal(error);
  }

  revalidatePath(`/console/orgs/${org}/permissions`, "layout");
  return { status: "idle" };
}
