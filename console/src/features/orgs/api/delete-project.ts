"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";

/**
 * What the delete control has to show: nothing yet, or a refusal. A
 * deleted project is the list it is sent back to.
 */
export type DeleteProjectState =
  { status: "idle" } | { status: "error"; message: string };

const Schema = z.object({
  org: z.string().min(1),
  project: z.string().min(1),
});

/**
 * Deletes a project of the organization. Its memberships go with it;
 * the users and agents that were its members are not touched.
 */
export async function deleteProject(
  _state: DeleteProjectState,
  formData: FormData,
): Promise<DeleteProjectState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    project: formData.get("project"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Name the project to delete." };
  }

  const { org, project } = parsed.data;
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/projects/${encodeURIComponent(project)}`,
      z.unknown(),
      { method: "DELETE" },
    );
  } catch (error) {
    return refusal(error);
  }

  revalidatePath(`/console/orgs/${org}/projects`, "layout");
  redirect(`/console/orgs/${org}/projects`);
}
