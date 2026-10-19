"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";

/**
 * What the delete control has to show: nothing yet, or a refusal. A
 * deleted operator is the list it is sent back to, and its absence
 * there is the whole of the outcome.
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
 * A refusal garam answers is rendered beside the control rather than
 * thrown.
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
    // A refusal is rendered beside the control rather than thrown: the
    // boundary would take the whole page down over one answer.
    return refusal(error);
  }

  revalidatePath(`/console/orgs/${org}/operators`, "layout");
  revalidatePath(`/console/orgs/${org}/agents`);
  redirect(`/console/orgs/${org}/operators`);
}
