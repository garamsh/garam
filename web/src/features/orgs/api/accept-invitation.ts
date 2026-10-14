"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";

import { joinedOrgPath } from "./invitation-preview";

/** What the accept form has to show: nothing yet, or a refusal. */
export type AcceptInvitationState =
  { status: "idle" } | { status: "error"; message: string };

const Schema = z.object({ token: z.string().min(1) });

/**
 * Accepts the invitation behind the token and lands in the organization
 * it named. The accept response carries no organization, so where to
 * land is read back from the invitation itself.
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
    // The membership the API answers with is not read: joining is what
    // was asked for, and the organization is what shows it.
    await garamFetchAuthenticated(
      `/invitations/${encodeURIComponent(parsed.data.token)}/accept`,
      z.unknown(),
      { method: "POST" },
    );
  } catch (error) {
    // An unreachable API is a state the form renders, not a throw: the
    // token is in the URL and no boundary covers this route, so throwing
    // replaces the console with `global-error.tsx` and the only retry
    // path the visitor holds is a page that no longer renders.
    return refusal(error, "acceptInvitation");
  }

  const destination = await joinedOrgPath(parsed.data.token);
  revalidatePath("/console", "layout");
  redirect(destination);
}
