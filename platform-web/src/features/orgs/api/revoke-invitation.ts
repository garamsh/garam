"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";

/** What revoking an invitation has to show: a refusal, or done. */
export type RevokeInvitationState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "revoked" };

const Schema = z.object({
  org: z.string().min(1),
  invitation: z.uuid(),
});

/**
 * Revokes an invitation no one has used, by its id, so its link admits
 * no one from the moment this returns (ADR-0136).
 */
export async function revokeInvitation(
  _state: RevokeInvitationState,
  formData: FormData,
): Promise<RevokeInvitationState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    invitation: formData.get("invitation"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Name the invitation to revoke." };
  }

  const { org, invitation } = parsed.data;
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/invitations/${invitation}`,
      z.unknown(),
      { method: "DELETE" },
    );
  } catch (error) {
    return refusal(error);
  }

  revalidatePath(`/console/orgs/${org}/people`);
  return { status: "revoked" };
}
