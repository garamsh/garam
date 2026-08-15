"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { z } from "zod";

import { GaramApiError, garamFetchAuthenticated } from "@/lib/garam";

import { slugFromGrn } from "../grn";
import type { Organization } from "./orgs";

export type CreateOrgState =
  | { status: "idle" }
  | { status: "error"; message: string };

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
    org = await garamFetchAuthenticated<Organization>("/orgs", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(parsed.data),
    });
  } catch (error) {
    if (error instanceof GaramApiError) {
      return { status: "error", message: error.message };
    }
    throw error;
  }

  revalidatePath("/console/orgs");
  redirect(`/console/orgs/${slugFromGrn(org.grn)}`);
}
