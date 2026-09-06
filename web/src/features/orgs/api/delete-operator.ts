"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
} from "@/lib/garam";

/**
 * What the delete control has to show: nothing yet, or a refusal.
 * There is no third state to report — the list the page revalidates is
 * what the delete changed, and an operator that is gone from it is the
 * whole of the outcome.
 */
export type DeleteOperatorState =
  { status: "idle" } | { status: "error"; message: string };

const Schema = z.object({
  org: z.string().min(1),
  operator: z.string().trim().min(1),
});

/**
 * Deletes the organization's record of the operator, which stops that
 * operator authenticating: the machine listener reads this record per
 * connection, so its certificate is refused from the moment this
 * returns (ADR-0070).
 *
 * garam refuses it while an agent definition names the operator, and
 * that refusal is rendered beside the record rather than thrown.
 */
export async function deleteOperator(
  _state: DeleteOperatorState,
  formData: FormData,
): Promise<DeleteOperatorState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    operator: formData.get("operator"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Name the operator to delete." };
  }

  const { org, operator } = parsed.data;
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/operators/${encodeURIComponent(operator)}`,
      z.unknown(),
      { method: "DELETE" },
    );
  } catch (error) {
    // A refusal is rendered beside the operator rather than thrown: the
    // boundary would take the whole list down over one record.
    if (
      error instanceof GaramApiError ||
      error instanceof GaramUnreachableError
    ) {
      return { status: "error", message: error.message };
    }
    throw error;
  }

  revalidatePath(`/console/orgs/${org}/operators`);
  return { status: "idle" };
}
