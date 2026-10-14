"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";

/**
 * What the delete control has to show: nothing yet, or a refusal. A
 * deleted organization is the list it is sent back to, and its absence
 * there is the whole of the outcome.
 */
export type DeleteOrgState =
  { status: "idle" } | { status: "error"; message: string };

const Schema = z.object({ org: z.string().min(1) });

/**
 * Deletes the organization and everything it holds, its certificate
 * authority included, which nothing restores (ADR-0065).
 */
export async function deleteOrg(
  _state: DeleteOrgState,
  formData: FormData,
): Promise<DeleteOrgState> {
  const parsed = Schema.safeParse({ org: formData.get("org") });
  if (!parsed.success) {
    return { status: "error", message: "Name the organization to delete." };
  }

  const { org } = parsed.data;
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}`,
      z.unknown(),
      { method: "DELETE" },
    );
  } catch (error) {
    return refusal(error);
  }

  revalidatePath("/console", "layout");
  redirect("/console/orgs");
}
