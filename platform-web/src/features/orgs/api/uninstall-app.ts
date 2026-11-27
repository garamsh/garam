"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";

/**
 * What the uninstall control has to show: nothing yet, or a refusal.
 * An uninstalled app is the list it is sent back to.
 */
export type UninstallAppState =
  { status: "idle" } | { status: "error"; message: string };

const Schema = z.object({
  org: z.string().min(1),
  installation: z.string().trim().min(1),
});

/**
 * Uninstalls the app: garam deletes the installation with its
 * instances, their keys and tokens, and every policy naming any of
 * them, whoever wrote it (ADR-0192 decision 4). A reinstall inherits
 * none of it.
 */
export async function uninstallApp(
  _state: UninstallAppState,
  formData: FormData,
): Promise<UninstallAppState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    installation: formData.get("installation"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Name the app to uninstall." };
  }

  const { org, installation } = parsed.data;
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/installations/${encodeURIComponent(installation)}`,
      z.unknown(),
      { method: "DELETE" },
    );
  } catch (error) {
    return refusal(error);
  }

  revalidatePath(`/console/orgs/${org}/apps`, "layout");
  redirect(`/console/orgs/${org}/apps`);
}
