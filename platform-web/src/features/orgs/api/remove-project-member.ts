"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";

import { parseGrn } from "@/lib/grn";

/** What a remove control has to show: nothing yet, or a refusal. */
export type RemoveProjectMemberState =
  { status: "idle" } | { status: "error"; message: string };

const Schema = z.object({
  org: z.string().min(1),
  project: z.string().min(1),
  member: z.string().refine((value) => parseGrn(value) !== null),
});

/**
 * Removes one membership of one project. The user or agent it named
 * stays in the organization and in every other project it belongs to —
 * the operation deletes a membership row and nothing else.
 */
export async function removeProjectMember(
  _state: RemoveProjectMemberState,
  formData: FormData,
): Promise<RemoveProjectMemberState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    project: formData.get("project"),
    member: formData.get("member"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Name the member to remove." };
  }

  const { org, project, member } = parsed.data;
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/projects/${encodeURIComponent(project)}/members/${encodeURIComponent(member)}`,
      z.unknown(),
      { method: "DELETE" },
    );
  } catch (error) {
    return refusal(error);
  }

  revalidatePath(`/console/orgs/${org}/projects/${project}`);
  return { status: "idle" };
}
