"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
  refusal,
} from "@/lib/garam";
import type { components } from "@garam/platform-sdk-ts/api";

import {
  IssuedOperatorKeyParser,
  type IssuedOperatorKey,
} from "./operator-keys";

/**
 * What issuing a key has to show: nothing yet, a refusal, a garam that
 * issues no keys because no token endpoint is configured, no answer,
 * or the key. garam refuses the unconfigured case with a reason of its
 * own (`platform/api/openapi.yaml` `Error.reason`), so it is told apart
 * from an outage rather than read as one (#1394).
 *
 * The private key is carried in this state because this answer is the
 * only place it exists: garam keeps the public key alone and answers
 * the private one once (ADR-0102). It goes nowhere else — no URL, no
 * revalidated render — and the client holds it in the wizard's state
 * until the wizard unmounts.
 */
export type IssueOperatorKeyState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "unconfigured" }
  | { status: "unavailable" }
  | {
      status: "issued";
      kid: string;
      privateKeyPem: string;
      clientId: string;
      tokenUrl: string;
    };

/** The reason garam refuses a key with where it issues none. */
const KEY_ISSUANCE_UNCONFIGURED = "key_issuance_unconfigured";

const Schema = z.object({
  org: z.string().min(1),
  operator: z.string().trim().min(1),
  rotation: z.enum(["planned", "immediate"]),
});

/**
 * Issues the operator a key (ADR-0102). `planned` adds it beside the
 * key in use, which retires 24 hours later; `immediate` withdraws every
 * live key and its tokens first. For an operator with no live key the
 * two are the same.
 */
export async function issueOperatorKey(
  _state: IssueOperatorKeyState,
  formData: FormData,
): Promise<IssueOperatorKeyState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    operator: formData.get("operator"),
    rotation: formData.get("rotation"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Choose how the new key stands." };
  }

  const { org, operator, rotation } = parsed.data;
  let issued: IssuedOperatorKey;
  try {
    issued = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/operators/${encodeURIComponent(operator)}/keys`,
      IssuedOperatorKeyParser,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          rotation,
        } satisfies components["schemas"]["OperatorKeyIssuance"]),
      },
    );
  } catch (error) {
    if (error instanceof GaramUnreachableError) {
      return { status: "unavailable" };
    }
    if (
      error instanceof GaramApiError &&
      error.reason?.code === KEY_ISSUANCE_UNCONFIGURED
    ) {
      return { status: "unconfigured" };
    }
    return refusal(error);
  }

  revalidatePath(`/console/orgs/${org}/operators/${operator}`, "layout");
  return {
    status: "issued",
    kid: issued.key.kid,
    privateKeyPem: issued.privateKeyPem,
    clientId: issued.clientId,
    tokenUrl: issued.tokenUrl,
  };
}
