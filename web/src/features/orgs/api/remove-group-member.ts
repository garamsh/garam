"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";

import { parseGrn } from "@/lib/grn";

/** What a remove control has to show: nothing yet, or a refusal. */
export type RemoveGroupMemberState =
  { status: "idle" } | { status: "error"; message: string };

const Schema = z.object({
  org: z.string().min(1),
  group: z.string().min(1),
  member: z.string().refine((value) => parseGrn(value) !== null),
});

/**
 * Removes one membership of one group. The user or agent it named
 * stays in the organization and in every other group it belongs to —
 * the operation deletes a membership row and nothing else.
 */
export async function removeGroupMember(
  _state: RemoveGroupMemberState,
  formData: FormData,
): Promise<RemoveGroupMemberState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    group: formData.get("group"),
    member: formData.get("member"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Name the member to remove." };
  }

  const { org, group, member } = parsed.data;
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/groups/${encodeURIComponent(group)}/members/${encodeURIComponent(member)}`,
      z.unknown(),
      { method: "DELETE" },
    );
  } catch (error) {
    return refusal(error);
  }

  revalidatePath(`/console/orgs/${org}/teams/${group}`);
  return { status: "idle" };
}
