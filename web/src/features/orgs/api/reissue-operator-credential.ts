"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
} from "@/lib/garam";

import { OperatorCredentialParser, type OperatorCredential } from "./orgs";

/**
 * What the re-issue control has to show: nothing yet, a refusal, or the
 * credential garam issued. The credential is carried here because this
 * answer is the only place it exists: garam stores its hash (ADR-0100).
 */
export type ReissueOperatorCredentialState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "issued"; credential: string };

const Schema = z.object({
  org: z.string().min(1),
  operator: z.string().trim().min(1),
});

/**
 * Issues the operator a new credential, which voids the one it held and
 * every token exchanged for it from the next request (ADR-0100).
 */
export async function reissueOperatorCredential(
  _state: ReissueOperatorCredentialState,
  formData: FormData,
): Promise<ReissueOperatorCredentialState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    operator: formData.get("operator"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Name the operator to re-issue for." };
  }

  const { org, operator } = parsed.data;
  let issued: OperatorCredential;
  try {
    issued = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/operators/${encodeURIComponent(operator)}/credential`,
      OperatorCredentialParser,
      { method: "POST" },
    );
  } catch (error) {
    // A refusal is rendered beside the control rather than thrown: the
    // boundary would take the whole page down over one answer.
    if (
      error instanceof GaramApiError ||
      error instanceof GaramUnreachableError
    ) {
      return { status: "error", message: error.message };
    }
    throw error;
  }

  revalidatePath(`/console/orgs/${org}/operators/${operator}`);
  return { status: "issued", credential: issued.credential };
}
