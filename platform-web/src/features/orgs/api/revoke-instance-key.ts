"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";

/** What revoking an instance's key has to show: nothing yet, a refusal, or done. */
export type RevokeInstanceKeyState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "revoked"; kid: string };

const Schema = z.object({
  org: z.string().min(1),
  installation: z.string().trim().min(1),
  instance: z.string().trim().min(1),
  kid: z.string().trim().min(1),
});

/**
 * Withdraws one of the instance's keys, and every token exchanged under
 * it, from the moment this returns (ADR-0184 decision 7). The key's
 * record stays, listed as revoked.
 */
export async function revokeInstanceKey(
  _state: RevokeInstanceKeyState,
  formData: FormData,
): Promise<RevokeInstanceKeyState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    installation: formData.get("installation"),
    instance: formData.get("instance"),
    kid: formData.get("kid"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Name the key to revoke." };
  }

  const { org, installation, instance, kid } = parsed.data;
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/installations/${encodeURIComponent(installation)}/instances/${encodeURIComponent(instance)}/keys/${encodeURIComponent(kid)}`,
      z.unknown(),
      { method: "DELETE" },
    );
  } catch (error) {
    return refusal(error);
  }

  revalidatePath(
    `/console/orgs/${org}/apps/${installation}/${instance}`,
    "layout",
  );
  return { status: "revoked", kid };
}
