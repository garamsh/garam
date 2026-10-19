"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";

/** What the revoke dialog shows: nothing yet, done, or a refusal. */
export type RevokeSessionState =
  | { status: "idle" }
  | { status: "revoked" }
  | { status: "error"; message: string };

const Schema = z.object({ session: z.string().min(1) });

/**
 * Revokes one of the person's sessions, so its tokens are refused from
 * the next request (ADR-0005, ADR-0120). Revoking the one this page was
 * opened on signs this browser out, which the next read finds.
 */
export async function revokeSession(
  _state: RevokeSessionState,
  formData: FormData,
): Promise<RevokeSessionState> {
  const parsed = Schema.safeParse({ session: formData.get("session") });
  if (!parsed.success) {
    return { status: "error", message: "This session can't be named." };
  }
  try {
    await garamFetchAuthenticated(
      `/me/sessions/${encodeURIComponent(parsed.data.session)}`,
      z.unknown(),
      { method: "DELETE" },
    );
  } catch (error) {
    return refusal(error, "revokeSession");
  }
  revalidatePath("/console/sessions");
  return { status: "revoked" };
}
