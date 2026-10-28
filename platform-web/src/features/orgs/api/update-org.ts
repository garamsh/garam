"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";
import type { components } from "@garam/platform-sdk-ts/api";

import { OrganizationParser } from "./orgs";

/**
 * What the rename form has to show: nothing yet, a refusal, or the name
 * saved.
 */
export type UpdateOrgState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "saved"; name: string };

const Schema = z.object({
  org: z.string().min(1),
  name: z.string().trim().min(1),
});

/**
 * Renames the organization. The identifier is in the path and not the
 * body, so every GRN under the organization stays as it was.
 */
export async function updateOrg(
  _state: UpdateOrgState,
  formData: FormData,
): Promise<UpdateOrgState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    name: formData.get("name"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Give the organization a name." };
  }

  const { org, name } = parsed.data;
  let saved: string;
  try {
    ({ name: saved } = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}`,
      OrganizationParser,
      {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          name,
        } satisfies components["schemas"]["OrganizationPatch"]),
      },
    ));
  } catch (error) {
    return refusal(error);
  }

  // The shell names the organization on every console page: the
  // switcher, the breadcrumb and the palette all read it.
  revalidatePath("/console", "layout");
  return { status: "saved", name: saved };
}
