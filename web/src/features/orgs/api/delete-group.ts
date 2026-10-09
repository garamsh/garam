"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";

/**
 * What the delete control has to show: nothing yet, or a refusal. A
 * deleted group is the list it is sent back to.
 */
export type DeleteGroupState =
  { status: "idle" } | { status: "error"; message: string };

const Schema = z.object({
  org: z.string().min(1),
  group: z.string().min(1),
});

/**
 * Deletes a group of the organization. Its memberships go with it; the
 * users and agents that were its members are not touched.
 */
export async function deleteGroup(
  _state: DeleteGroupState,
  formData: FormData,
): Promise<DeleteGroupState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    group: formData.get("group"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Name the team to delete." };
  }

  const { org, group } = parsed.data;
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/groups/${encodeURIComponent(group)}`,
      z.unknown(),
      { method: "DELETE" },
    );
  } catch (error) {
    return refusal(error);
  }

  revalidatePath(`/console/orgs/${org}/teams`, "layout");
  redirect(`/console/orgs/${org}/teams`);
}
