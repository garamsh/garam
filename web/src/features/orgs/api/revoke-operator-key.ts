"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";

/** What revoking a key has to show: nothing yet, a refusal, or done. */
export type RevokeOperatorKeyState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "revoked"; kid: string };

const Schema = z.object({
  org: z.string().min(1),
  operator: z.string().trim().min(1),
  kid: z.string().trim().min(1),
});

/**
 * Withdraws one of the operator's keys, and every token exchanged under
 * it, from the moment this returns (ADR-0102). The key's record stays,
 * listed as revoked.
 */
export async function revokeOperatorKey(
  _state: RevokeOperatorKeyState,
  formData: FormData,
): Promise<RevokeOperatorKeyState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    operator: formData.get("operator"),
    kid: formData.get("kid"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Name the key to revoke." };
  }

  const { org, operator, kid } = parsed.data;
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/operators/${encodeURIComponent(operator)}/keys/${encodeURIComponent(kid)}`,
      z.unknown(),
      { method: "DELETE" },
    );
  } catch (error) {
    return refusal(error);
  }

  revalidatePath(`/console/orgs/${org}/operators/${operator}`, "layout");
  return { status: "revoked", kid };
}
