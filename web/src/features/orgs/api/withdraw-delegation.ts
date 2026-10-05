"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
} from "@/lib/garam";
import {
  CONTRACT_VERSION,
  CONTRACT_VERSION_HEADER,
} from "@/lib/operation-authority";
import type { components } from "@/types/api";

import type { DelegationState } from "./grant-delegation";
import { orgPath } from "./orgs";

const EndSchema = z.object({
  org: z.string().min(1),
  operator: z.string().min(1),
  grantId: z.string().min(1),
});

/** Withdraws the grant the page showed, and only that one. */
export async function withdrawDelegation(
  _state: DelegationState,
  formData: FormData,
): Promise<DelegationState> {
  const parsed = EndSchema.safeParse({
    org: formData.get("org"),
    operator: formData.get("operator"),
    grantId: formData.get("grantId"),
  });
  if (!parsed.success) {
    return { status: "error", message: "The form did not arrive complete." };
  }
  const { org, operator, grantId } = parsed.data;
  try {
    await garamFetchAuthenticated(delegationPath(org, operator), z.unknown(), {
      method: "DELETE",
      headers: {
        "content-type": "application/json",
        [CONTRACT_VERSION_HEADER]: CONTRACT_VERSION,
      },
      body: JSON.stringify({
        grantId,
      } satisfies components["schemas"]["DelegationEnd"]),
    });
  } catch (error) {
    return refusal(error);
  }
  revalidatePath(`/console/orgs/${org}`, "layout");
  return { status: "withdrawn" };
}

function delegationPath(org: string, operator: string): string {
  return `/orgs/${orgPath(org)}/operators/${orgPath(operator)}/delegation`;
}

function refusal(error: unknown): DelegationState {
  if (
    error instanceof GaramApiError ||
    error instanceof GaramUnreachableError
  ) {
    return { status: "error", message: error.message };
  }
  throw error;
}
