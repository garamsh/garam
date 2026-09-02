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
import { OrganizationParser, type Organization } from "./orgs";

/** What the create form has to show: nothing yet, or a refusal. */
export type CreateOrgState =
  { status: "idle" } | { status: "error"; message: string };

const Schema = z.object({ name: z.string().trim().min(1) });

/** Creates an organization; the caller becomes its first owner. */
export async function createOrg(
  _state: CreateOrgState,
  formData: FormData,
): Promise<CreateOrgState> {
  const parsed = Schema.safeParse({ name: formData.get("name") });
  if (!parsed.success) {
    return { status: "error", message: "Give the organization a name." };
  }

  let org: Organization;
  try {
    org = await garamFetchAuthenticated("/orgs", OrganizationParser, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(
        parsed.data satisfies components["schemas"]["OrganizationCreate"],
      ),
    });
  } catch (error) {
    // An unreachable API is a state the form renders, not a throw: the
    // boundary would replace the form and the name typed into it.
    if (
      error instanceof GaramApiError ||
      error instanceof GaramUnreachableError
    ) {
      return { status: "error", message: error.message };
    }
    throw error;
  }

  revalidatePath("/console/orgs");
  redirect(`/console/orgs/${slugFromGrn(org.grn)}`);
}
