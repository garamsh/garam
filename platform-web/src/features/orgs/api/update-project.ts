"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";
import type { components } from "@garam/platform-sdk-ts/api";

import { ProjectParser } from "./orgs";

/**
 * What the rename form has to show: nothing yet, a refusal, or the name
 * saved.
 */
export type UpdateProjectState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "saved"; name: string };

const Schema = z.object({
  org: z.string().min(1),
  project: z.string().min(1),
  name: z.string().trim().min(1),
});

/**
 * Renames a project of the organization. Only the name is sent, so the
 * roles the project grants are left as they stand.
 */
export async function updateProject(
  _state: UpdateProjectState,
  formData: FormData,
): Promise<UpdateProjectState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    project: formData.get("project"),
    name: formData.get("name"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Give the project a name." };
  }

  const { org, project, name } = parsed.data;
  let saved: string;
  try {
    ({ name: saved } = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/projects/${encodeURIComponent(project)}`,
      ProjectParser,
      {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          name,
        } satisfies components["schemas"]["ProjectPatch"]),
      },
    ));
  } catch (error) {
    return refusal(error);
  }

  revalidatePath(`/console/orgs/${org}/projects`, "layout");
  return { status: "saved", name: saved };
}
