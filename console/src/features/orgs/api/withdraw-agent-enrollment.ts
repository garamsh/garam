"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";

/** What the control has to show: nothing yet, a refusal, or done. */
export type WithdrawAgentEnrollmentState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "done" };

const Schema = z.object({
  org: z.string().min(1),
  enrollment: z.string().min(1),
});

/**
 * Withdraws an approval no host has spent: spending it afterwards is
 * refused as an unknown bootstrap would be (ADR-0082).
 */
export async function withdrawAgentEnrollment(
  _state: WithdrawAgentEnrollmentState,
  formData: FormData,
): Promise<WithdrawAgentEnrollmentState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    enrollment: formData.get("enrollment"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Name the approval to withdraw." };
  }

  const { org, enrollment } = parsed.data;
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/agent-enrollments/${encodeURIComponent(enrollment)}`,
      z.unknown(),
      { method: "DELETE" },
    );
  } catch (error) {
    return refusal(error, "withdrawAgentEnrollment");
  }

  revalidatePath(`/console/orgs/${org}/hosts`);
  revalidatePath(`/console/orgs/${org}/agents`, "layout");
  return { status: "done" };
}
