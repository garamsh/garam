"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";

/** What the control has to show: nothing yet, a refusal, or done. */
export type RevokeAgentSupervisorState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "done" };

const Schema = z.object({
  org: z.string().min(1),
  supervisor: z.string().min(1),
});

/**
 * Revokes a standalone host's supervisor: its registration, its agent's
 * activation and every unspent approval for that agent end together,
 * and the agent and its memory stay (ADR-0082).
 */
export async function revokeAgentSupervisor(
  _state: RevokeAgentSupervisorState,
  formData: FormData,
): Promise<RevokeAgentSupervisorState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    supervisor: formData.get("supervisor"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Name the supervisor to revoke." };
  }

  const { org, supervisor } = parsed.data;
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/agent-supervisors/${encodeURIComponent(supervisor)}`,
      z.unknown(),
      { method: "DELETE" },
    );
  } catch (error) {
    return refusal(error, "revokeAgentSupervisor");
  }

  revalidatePath(`/console/orgs/${org}/hosts`);
  revalidatePath(`/console/orgs/${org}/agents`, "layout");
  return { status: "done" };
}
