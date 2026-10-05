"use server";

import { z } from "zod";

import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
} from "@/lib/garam";
import {
  CONTRACT_VERSION,
  CONTRACT_VERSION_HEADER,
  type MintInput,
  type MintResult,
} from "@/lib/operation-authority";
import type { components } from "@/types/api";

import { orgPath } from "./orgs";

const Schema = z.object({
  org: z.string().min(1),
  audience: z.string().min(1),
  operation: z.enum([
    "agent:create",
    "agent-template:read",
    "agent-template:publish",
    "execution-profile:read",
    "agent:execution-read",
  ]),
  target: z.string().min(1),
  requestId: z.string().min(1),
  bodySha256: z.string().min(1),
  requestTarget: z.string().min(1).optional(),
});

const AuthorityParser = z.object({ authority: z.string().min(1) });

/**
 * Mints one operation authority for the signed-in user, which the
 * browser sends to the hosted operator's control service and nowhere
 * else (#1170). garam decides it from current membership, roles, groups,
 * policies and the operator's grant, and binds it to the exact request.
 *
 * The authority crosses to the browser in this answer and in no other
 * way: it is not stored, logged or put in a URL, and it lives five
 * minutes at most.
 */
export async function mintOperationAuthority(
  input: MintInput,
): Promise<MintResult> {
  const parsed = Schema.safeParse(input);
  if (!parsed.success) {
    return { status: "refused", message: "The request did not arrive complete." };
  }
  const { org, ...request } = parsed.data;
  try {
    const { authority } = await garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/operation-authorities`,
      AuthorityParser,
      {
        method: "POST",
        headers: {
          "content-type": "application/json",
          [CONTRACT_VERSION_HEADER]: CONTRACT_VERSION,
        },
        body: JSON.stringify(
          request satisfies components["schemas"]["OperationAuthorityRequest"],
        ),
      },
    );
    return { status: "ok", authority };
  } catch (error) {
    if (error instanceof GaramUnreachableError) return { status: "unavailable" };
    if (error instanceof GaramApiError) {
      return { status: "refused", message: error.message };
    }
    throw error;
  }
}
