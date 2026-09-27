"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
} from "@/lib/garam";
import type { components } from "@/types/api";

import { GroupParser } from "./orgs";

/**
 * What the rename form has to show: nothing yet, a refusal, or the name
 * saved.
 */
export type UpdateGroupState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "saved"; name: string };

const Schema = z.object({
  org: z.string().min(1),
  group: z.string().min(1),
  name: z.string().trim().min(1),
});

/**
 * Renames a group of the organization. Only the name is sent, so the
 * roles the group grants are left as they stand (`api/openapi.yaml`
 * §ScopedRoles).
 */
export async function updateGroup(
  _state: UpdateGroupState,
  formData: FormData,
): Promise<UpdateGroupState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    group: formData.get("group"),
    name: formData.get("name"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Give the team a name." };
  }

  const { org, group, name } = parsed.data;
  let saved: string;
  try {
    ({ name: saved } = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/groups/${encodeURIComponent(group)}`,
      GroupParser,
      {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          name,
        } satisfies components["schemas"]["GroupPatch"]),
      },
    ));
  } catch (error) {
    if (
      error instanceof GaramApiError ||
      error instanceof GaramUnreachableError
    ) {
      return { status: "error", message: error.message };
    }
    throw error;
  }

  revalidatePath(`/console/orgs/${org}/teams`, "layout");
  return { status: "saved", name: saved };
}
