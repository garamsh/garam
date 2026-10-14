import "server-only";

import { z } from "zod";

import {
  GaramApiError,
  GaramUnreachableError,
  garamFetch,
  parserFor,
} from "@/lib/garam";
import type { components } from "@/types/api";

/**
 * What an invitation admits its holder to, as the API answers it: the
 * organization by identifier and name, the role, and whether it can
 * still be used.
 */
export type InvitationPreview = components["schemas"]["InvitationPreview"];

/** The read's answers: the invitation, no such token, or no answer. */
export type InvitationPreviewRead =
  | { status: "found"; invitation: InvitationPreview }
  | { status: "not-found" }
  | { status: "unreachable" };

const InvitationPreviewParser = parserFor<InvitationPreview>()(
  z.object({
    org: z.string(),
    orgName: z.string(),
    role: z.enum(["owner", "admin", "member"]),
    state: z.enum(["pending", "accepted", "expired"]),
    expiresAt: z.string(),
  }),
);

/**
 * Reads the invitation behind `token`. It needs no session, so the
 * sign-in and register pages can say what an invitation is for before
 * an account exists to accept it with (ADR-0006).
 *
 * Read where a page renders and nowhere earlier: the token is the
 * whole claim, so nothing prefetches it or puts it anywhere a URL the
 * console builds does not already hold it.
 */
export async function getInvitationPreview(
  token: string,
): Promise<InvitationPreviewRead> {
  try {
    return {
      status: "found",
      invitation: await garamFetch(
        `/invitations/${encodeURIComponent(token)}`,
        InvitationPreviewParser,
      ),
    };
  } catch (error) {
    if (error instanceof GaramApiError && error.status === 404) {
      return { status: "not-found" };
    }
    if (error instanceof GaramUnreachableError) {
      return { status: "unreachable" };
    }
    throw error;
  }
}

/**
 * Where a person who just joined through `token` lands: the
 * organization the invitation named, read back from garam rather than
 * taken from the form, or the console's front page where the read has
 * no answer.
 */
export async function joinedOrgPath(token: string): Promise<string> {
  const read = await getInvitationPreview(token);
  return read.status === "found"
    ? `/console/orgs/${encodeURIComponent(read.invitation.org)}`
    : "/console";
}
