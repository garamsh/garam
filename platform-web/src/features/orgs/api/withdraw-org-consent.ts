"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";

/**
 * What the withdraw control has to show: nothing yet, or a refusal.
 * There is no third state to report — the relation the list already
 * renders is what withdrawing changed, and the revalidated list says
 * whether the pair fell or the other half is still up.
 */
export type WithdrawOrgConsentState =
  { status: "idle" } | { status: "error"; message: string };

const Schema = z.object({
  org: z.string().min(1),
  other: z.string().trim().min(1),
});

/**
 * Clears this organization's own half of the pair. The other half is
 * untouched and keeps standing for a later re-grant to join; the
 * relation itself falls at once, so a message between the two is
 * refused back to its sender from here on (ADR-0035).
 */
export async function withdrawOrgConsent(
  _state: WithdrawOrgConsentState,
  formData: FormData,
): Promise<WithdrawOrgConsentState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    other: formData.get("other"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Name the other organization." };
  }

  const { org, other } = parsed.data;
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/consents/${encodeURIComponent(other)}`,
      z.unknown(),
      { method: "DELETE" },
    );
  } catch (error) {
    // A refusal is rendered beside the relation rather than thrown: the
    // boundary would take the whole list down over one row.
    return refusal(error);
  }

  revalidatePath(`/console/orgs/${org}/consents`);
  return { status: "idle" };
}
