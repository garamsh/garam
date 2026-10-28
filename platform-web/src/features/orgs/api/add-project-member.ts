"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { GaramApiError, garamFetchAuthenticated, refusal } from "@/lib/garam";
import type { components } from "@garam/platform-sdk-ts/api";

import { parseGrn } from "@/lib/grn";

/** What the add form has to show: nothing yet, or a refusal. */
export type AddProjectMemberState =
  { status: "idle" } | { status: "error"; message: string };

const Schema = z.object({
  org: z.string().min(1),
  project: z.string().min(1),
  member: z.string().refine((value) => parseGrn(value) !== null),
});

/**
 * Adds a user or an agent to one project of the organization. Nothing
 * else changes: the member's other projects and its place in the
 * organization are its own.
 */
export async function addProjectMember(
  _state: AddProjectMemberState,
  formData: FormData,
): Promise<AddProjectMemberState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    project: formData.get("project"),
    member: formData.get("member"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Choose a person or an agent to add." };
  }

  const { org, project, member } = parsed.data;
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/projects/${encodeURIComponent(project)}/members`,
      z.unknown(),
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          memberGrn: member,
        } satisfies components["schemas"]["MemberRef"]),
      },
    );
  } catch (error) {
    // A conflict is a member added since the page was read: the list
    // is refreshed so it shows the member the refusal names.
    if (error instanceof GaramApiError && error.status === 409) {
      revalidatePath(`/console/orgs/${org}/projects/${project}`);
      return {
        status: "error",
        message: "Already a member of this project. The list now shows it.",
      };
    }
    return refusal(error);
  }

  revalidatePath(`/console/orgs/${org}/projects/${project}`);
  return { status: "idle" };
}
