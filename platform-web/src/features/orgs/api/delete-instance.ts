"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";

/**
 * What the delete control has to show: nothing yet, or a refusal. A
 * deleted instance is the installation it is sent back to.
 */
export type DeleteInstanceState =
  { status: "idle" } | { status: "error"; message: string };

const Schema = z.object({
  org: z.string().min(1),
  installation: z.string().trim().min(1),
  instance: z.string().trim().min(1),
});

/**
 * Deletes the organization's record of the instance, and with it what
 * the instance authenticates as (ADR-0184 decision 7). Nothing in the
 * instance is touched: garam never dials it.
 */
export async function deleteInstance(
  _state: DeleteInstanceState,
  formData: FormData,
): Promise<DeleteInstanceState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    installation: formData.get("installation"),
    instance: formData.get("instance"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Name the instance to delete." };
  }

  const { org, installation, instance } = parsed.data;
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/installations/${encodeURIComponent(installation)}/instances/${encodeURIComponent(instance)}`,
      z.unknown(),
      { method: "DELETE" },
    );
  } catch (error) {
    return refusal(error);
  }

  revalidatePath(`/console/orgs/${org}/apps/${installation}`, "layout");
  redirect(`/console/orgs/${org}/apps/${encodeURIComponent(installation)}`);
}
