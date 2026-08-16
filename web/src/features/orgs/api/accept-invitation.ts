"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { z } from "zod";

import { GaramApiError, garamFetchAuthenticated } from "@/lib/garam";

import type { Member } from "./orgs";

/** What the accept form has to show: nothing yet, or a refusal. */
export type AcceptInvitationState =
  | { status: "idle" }
  | { status: "error"; message: string };

const Schema = z.object({ token: z.string().min(1) });

/**
 * Accepts the invitation behind the token. Lands on the organization
 * list: the accept response carries no org identifier to deep-link to.
 */
export async function acceptInvitation(
  _state: AcceptInvitationState,
  formData: FormData,
): Promise<AcceptInvitationState> {
  const parsed = Schema.safeParse({ token: formData.get("token") });
  if (!parsed.success) {
    return { status: "error", message: "The invitation link is incomplete." };
  }

  try {
    await garamFetchAuthenticated<Member>(
      `/invitations/${encodeURIComponent(parsed.data.token)}/accept`,
      { method: "POST" },
    );
  } catch (error) {
    if (error instanceof GaramApiError) {
      return { status: "error", message: error.message };
    }
    throw error;
  }

  revalidatePath("/console/orgs");
  redirect("/console/orgs");
}
