"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { GaramApiError, garamFetchAuthenticated, refusal } from "@/lib/garam";
import type { components } from "@garam/platform-sdk-ts/api";

import { parseGrn } from "@/lib/grn";
import { PolicyAttachmentParser } from "./policies";

/** What an attach form has to show: nothing yet, or a refusal. */
export type AttachPolicyState =
  { status: "idle" } | { status: "error"; message: string };

const Schema = z.object({
  org: z.string().min(1),
  policy: z.string().min(1),
  subject: z.string().refine((value) => parseGrn(value) !== null),
});

/**
 * Attaches a policy to a team, a person or an agent of the
 * organization. The policy then applies to its attached subjects
 * rather than to the whole organization; the first attachment of an
 * active policy narrows it.
 */
export async function attachPolicy(
  _state: AttachPolicyState,
  formData: FormData,
): Promise<AttachPolicyState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    policy: formData.get("policy"),
    subject: formData.get("subject"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Choose what to attach." };
  }

  const { org, policy, subject } = parsed.data;
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/policies/${encodeURIComponent(policy)}/attachments`,
      PolicyAttachmentParser,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          subjectGrn: subject,
        } satisfies components["schemas"]["PolicyAttachmentCreate"]),
      },
    );
  } catch (error) {
    // An attachment made since the page was read: the page is
    // refreshed so it shows the one the refusal names.
    if (error instanceof GaramApiError && error.status === 409) {
      revalidatePath(`/console/orgs/${org}/permissions`, "layout");
      return {
        status: "error",
        message: "Already attached. The page now shows it.",
      };
    }
    return refusal(error);
  }

  revalidatePath(`/console/orgs/${org}/permissions`, "layout");
  return { status: "idle" };
}
