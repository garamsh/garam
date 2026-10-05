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

import { DelegationParser } from "./delegation";
import { orgPath } from "./orgs";

/** What the delegation form has to show. */
export type DelegationState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "granted"; grantId: string }
  | { status: "withdrawn" };

/** The longest grant garam accepts, in days (#1160). */
const MAX_DAYS = 30;

const GrantSchema = z.object({
  org: z.string().min(1),
  operator: z.string().min(1),
  expectedGrantId: z.string(),
  controllers: z.array(z.string().min(1)).min(1),
  operations: z.array(z.string().min(1)).min(1),
  days: z.coerce.number().int().min(1).max(MAX_DAYS),
});

/**
 * Grants or replaces the hosted operator's delegation (ADR-0078). The
 * grant the form read is sent as the expected one, so a write made
 * from a view another write has since moved is refused rather than
 * overwriting it.
 */
export async function grantDelegation(
  _state: DelegationState,
  formData: FormData,
): Promise<DelegationState> {
  const parsed = GrantSchema.safeParse({
    org: formData.get("org"),
    operator: formData.get("operator"),
    expectedGrantId: formData.get("expectedGrantId") ?? "",
    controllers: formData.getAll("controllers"),
    operations: formData.getAll("operations"),
    days: formData.get("days"),
  });
  if (!parsed.success) {
    return {
      status: "error",
      message: `Choose at least one controller and one operation, for 1 to ${MAX_DAYS} days.`,
    };
  }
  const { org, operator, expectedGrantId, controllers, operations, days } =
    parsed.data;
  const body = {
    requestId: crypto.randomUUID(),
    controllers,
    operations: operations as components["schemas"]["DelegatedOperation"][],
    expiresAt: new Date(Date.now() + days * 24 * 60 * 60 * 1000).toISOString(),
    ...(expectedGrantId === "" ? {} : { expectedGrantId }),
  } satisfies components["schemas"]["DelegationWrite"];
  let grantId: string;
  try {
    ({ grantId } = await garamFetchAuthenticated(
      delegationPath(org, operator),
      DelegationParser,
      {
        method: "PUT",
        headers: {
          "content-type": "application/json",
          [CONTRACT_VERSION_HEADER]: CONTRACT_VERSION,
        },
        body: JSON.stringify(body),
      },
    ));
  } catch (error) {
    return refusal(error);
  }
  revalidatePath(`/console/orgs/${org}`, "layout");
  return { status: "granted", grantId };
}

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
