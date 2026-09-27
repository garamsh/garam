"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
} from "@/lib/garam";
import type { components } from "@/types/api";

import { parseGrn } from "../grn";

/** What the add form has to show: nothing yet, or a refusal. */
export type AddGroupMemberState =
  { status: "idle" } | { status: "error"; message: string };

const Schema = z.object({
  org: z.string().min(1),
  group: z.string().min(1),
  member: z.string().refine((value) => parseGrn(value) !== null),
});

/**
 * Adds a user or an agent to one group of the organization. Nothing
 * else changes: the member's other groups and its place in the
 * organization are its own.
 */
export async function addGroupMember(
  _state: AddGroupMemberState,
  formData: FormData,
): Promise<AddGroupMemberState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    group: formData.get("group"),
    member: formData.get("member"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Choose a person or an agent to add." };
  }

  const { org, group, member } = parsed.data;
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/groups/${encodeURIComponent(group)}/members`,
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
      revalidatePath(`/console/orgs/${org}/teams/${group}`);
      return {
        status: "error",
        message: "Already a member of this team. The list now shows it.",
      };
    }
    if (
      error instanceof GaramApiError ||
      error instanceof GaramUnreachableError
    ) {
      return { status: "error", message: error.message };
    }
    throw error;
  }

  revalidatePath(`/console/orgs/${org}/teams/${group}`);
  return { status: "idle" };
}
