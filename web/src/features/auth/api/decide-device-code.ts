"use server";

import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";

/** What the `/device` form shows: nothing yet, the decision, or a refusal. */
export type DecideDeviceCodeState =
  | { status: "idle" }
  | { status: "approved" | "denied" }
  | { status: "error"; message: string };

const Schema = z.object({
  userCode: z.string().trim().min(1),
  decision: z.enum(["approve", "deny"]),
});

/**
 * Approves or denies the device sign-in the code names, as the
 * signed-in person (ADR-0120). Approving signs that CLI in as them; the
 * page has said so before this runs. A code garam holds nothing pending
 * for is a refusal the form renders, beside what was typed.
 */
export async function decideDeviceCode(
  _state: DecideDeviceCodeState,
  formData: FormData,
): Promise<DecideDeviceCodeState> {
  const parsed = Schema.safeParse({
    userCode: formData.get("userCode"),
    decision: formData.get("decision"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Enter the code your terminal shows." };
  }
  const { userCode, decision } = parsed.data;
  try {
    await garamFetchAuthenticated("/auth/device/approval", z.unknown(), {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ userCode, approve: decision === "approve" }),
    });
  } catch (error) {
    return refusal(error, "decideDeviceCode");
  }
  return { status: decision === "approve" ? "approved" : "denied" };
}
