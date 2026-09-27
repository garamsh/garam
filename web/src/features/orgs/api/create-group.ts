"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { z } from "zod";

import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
} from "@/lib/garam";
import type { components } from "@/types/api";

import { slugFromGrn } from "../grn";
import { GroupParser, type Group } from "./orgs";

/**
 * What the create form has to show: nothing yet, or a refusal. There is
 * no created state — a created group is the page it is sent to.
 */
export type CreateGroupState =
  { status: "idle" } | { status: "error"; message: string };

const Schema = z.object({
  org: z.string().min(1),
  name: z.string().trim().min(1),
});

/**
 * Creates a group of the organization — a team, in the console — and
 * opens the page of the one garam recorded. The name is the only field
 * sent: `roles` is optional on the operation and nothing here chooses
 * one.
 */
export async function createGroup(
  _state: CreateGroupState,
  formData: FormData,
): Promise<CreateGroupState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    name: formData.get("name"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Give the team a name." };
  }

  const { org, name } = parsed.data;
  let created: Group;
  try {
    created = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/groups`,
      GroupParser,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          name,
        } satisfies components["schemas"]["GroupCreate"]),
      },
    );
  } catch (error) {
    // A refusal or an outage is a state the form renders, not a throw:
    // the boundary would replace the form and the name typed into it.
    if (
      error instanceof GaramApiError ||
      error instanceof GaramUnreachableError
    ) {
      return { status: "error", message: error.message };
    }
    throw error;
  }

  revalidatePath(`/console/orgs/${org}/teams`);
  redirect(`/console/orgs/${org}/teams/${slugFromGrn(created.grn)}`);
}
