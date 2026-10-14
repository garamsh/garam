"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";

import { slugFromGrn } from "@/lib/grn";
import { OrgConsentParser, type OrgConsent } from "./orgs";

/**
 * What the grant form has to show: nothing yet, a refusal, or the
 * relation as it now stands. `stands` is the pair's state and not this
 * organization's — a granted half waits until the other one lands
 * (ADR-0035), which is what the form says in place of "done".
 */
export type GrantOrgConsentState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "granted"; other: string; stands: boolean };

const Schema = z.object({
  org: z.string().min(1),
  other: z.string().trim().min(1),
});

/**
 * Stands this organization's half of the pair it shares with `other`.
 * Which half of the stored row moves is the API's to decide, from where
 * the granter sorts (ADR-0035); this write names the two organizations
 * and reads back the relation.
 *
 * The identifier is not parsed here: the console builds no GRN from it,
 * and an organization garam does not hold, or this one itself, is
 * refused by the API — a refusal the form renders in this operation's
 * own sentence.
 */
export async function grantOrgConsent(
  _state: GrantOrgConsentState,
  formData: FormData,
): Promise<GrantOrgConsentState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    other: formData.get("other"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Name the other organization." };
  }

  const { org, other } = parsed.data;
  let consent: OrgConsent;
  try {
    consent = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/consents/${encodeURIComponent(other)}`,
      OrgConsentParser,
      { method: "PUT" },
    );
  } catch (error) {
    // An unreachable API is a state the form renders, not a throw: the
    // boundary would replace the form and the identifier typed into it.
    return refusal(error, "grantOrgConsent");
  }

  revalidatePath(`/console/orgs/${org}/consents`);
  return {
    status: "granted",
    other: slugFromGrn(consent.orgGrn),
    stands: consent.stands,
  };
}
