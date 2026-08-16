"use server";

import { z } from "zod";

import { GaramApiError, garamFetchAuthenticated } from "@/lib/garam";
import type { components } from "@/types/api";

import type { Invitation } from "./orgs";

/**
 * What the invite form has to show: nothing yet, a refusal, or the
 * token the invitation was issued under.
 */
export type CreateInvitationState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "created"; token: string };

// Owners are not transferable by invitation (api/openapi.yaml).
const Schema = z.object({
  org: z.string().min(1),
  role: z.enum(["admin", "member"]),
});

/** Creates an invitation to the org and returns its bearer token. */
export async function createInvitation(
  _state: CreateInvitationState,
  formData: FormData,
): Promise<CreateInvitationState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    role: formData.get("role"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Choose a role for the invitation." };
  }

  try {
    const invitation = await garamFetchAuthenticated<Invitation>(
      `/orgs/${encodeURIComponent(parsed.data.org)}/invitations`,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          role: parsed.data.role,
        } satisfies components["schemas"]["InvitationCreate"]),
      },
    );
    return { status: "created", token: invitation.token };
  } catch (error) {
    if (error instanceof GaramApiError) {
      return { status: "error", message: error.message };
    }
    throw error;
  }
}
