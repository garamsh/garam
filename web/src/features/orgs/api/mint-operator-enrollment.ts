"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
  parserFor,
} from "@/lib/garam";
import type { components } from "@/types/api";

/**
 * What the mint control has to show: nothing yet, a refusal, garam's
 * refusal for an operator that already enrolled, or the token just
 * minted and when it stops being spendable.
 *
 * The token is carried in this state because the answer that minted it
 * is the only place it exists: garam stores its hash, so nothing can
 * fetch it again once this state is replaced (ADR-0064).
 */
export type MintOperatorEnrollmentState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "already-enrolled" }
  | { status: "minted"; token: string; expiresAt: string };

const OperatorEnrollmentParser = parserFor<
  components["schemas"]["OperatorEnrollment"]
>()(
  z.object({
    token: z.string().min(1),
    expiresAt: z.string(),
  }),
);

const Schema = z.object({
  org: z.string().min(1),
  operator: z.string().min(1),
});

/**
 * Mints a registered operator a new one-time enrollment token, which
 * replaces any token minted before it (ADR-0067).
 *
 * garam refuses it with a 409 for an operator that holds a
 * certificate; that refusal is its own state, so the control can say
 * it in a sentence of its own rather than garam's. Minting says
 * nothing about whether the operator picks the token up.
 */
export async function mintOperatorEnrollment(
  _state: MintOperatorEnrollmentState,
  formData: FormData,
): Promise<MintOperatorEnrollmentState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    operator: formData.get("operator"),
  });
  if (!parsed.success) {
    return { status: "error", message: "The form did not arrive complete." };
  }

  const { org, operator } = parsed.data;
  const section = `/console/orgs/${org}/operators`;
  let minted: components["schemas"]["OperatorEnrollment"];
  try {
    minted = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/operators/${encodeURIComponent(operator)}/enrollment`,
      OperatorEnrollmentParser,
      { method: "POST" },
    );
  } catch (error) {
    // The operator enrolled since the page was read, so the page is
    // re-read to show the certificate it now holds.
    if (error instanceof GaramApiError && error.status === 409) {
      revalidatePath(section, "layout");
      return { status: "already-enrolled" };
    }
    if (
      error instanceof GaramApiError ||
      error instanceof GaramUnreachableError
    ) {
      return { status: "error", message: error.message };
    }
    throw error;
  }

  revalidatePath(section, "layout");
  return {
    status: "minted",
    token: minted.token,
    expiresAt: minted.expiresAt,
  };
}
