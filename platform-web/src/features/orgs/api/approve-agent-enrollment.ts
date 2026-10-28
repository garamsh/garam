"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";
import { parseGrn } from "@/lib/grn";
import { REFUSAL_COPY } from "@garam/platform-sdk-ts/refusal-copy";
import type { components } from "@garam/platform-sdk-ts/api";

/** What the approval dialog has to show: nothing yet, a refusal, or done. */
export type ApproveAgentEnrollmentState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "approved"; hostName: string };

/** The longest an approval may stay spendable (`platform/api/openapi.yaml`). */
const MAX_APPROVAL_HOURS = 24;

const Hex64 = z.string().regex(/^[0-9a-f]{64}$/);

/**
 * What `garam standalone prepare` prints, and nothing else: a field it
 * does not print, or one missing, is refused rather than guessed at.
 */
const Prepared = z
  .object({
    requestId: z.string().regex(/^[A-Za-z0-9._~-]{1,128}$/),
    secretSha256: Hex64,
  })
  .strict();

const Grn = z.string().refine((value) => parseGrn(value) !== null);

const Schema = z.object({
  org: z.string().min(1),
  hostName: z.string().trim().min(1).max(100),
  placement: z.enum(["standalone", "direct"]),
  hours: z.coerce.number().int().min(1).max(MAX_APPROVAL_HOURS),
  replacesAgent: Grn.optional(),
  replacesAssignee: Grn.optional(),
  replacesEpoch: z.coerce.number().int().min(1).optional(),
});

/** Reads the pasted output, or null where it is not prepare's. */
function readPrepared(text: string): z.infer<typeof Prepared> | null {
  try {
    const parsed = Prepared.safeParse(JSON.parse(text));
    return parsed.success ? parsed.data : null;
  } catch {
    return null;
  }
}

/**
 * Approves what a standalone or direct host prepared (ADR-0082,
 * ADR-0111): the hash of its bootstrap secret and its request
 * identifier, pasted as `garam standalone prepare` printed them
 * (ADR-0118). A replacement names the assignment
 * it replaces, exactly as it stands. Nothing pasted is said back.
 */
export async function approveAgentEnrollment(
  _state: ApproveAgentEnrollmentState,
  formData: FormData,
): Promise<ApproveAgentEnrollmentState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    hostName: formData.get("hostName"),
    placement: formData.get("placement"),
    hours: formData.get("hours"),
    replacesAgent: formData.get("replacesAgent") || undefined,
    replacesAssignee: formData.get("replacesAssignee") || undefined,
    replacesEpoch: formData.get("replacesEpoch") || undefined,
  });
  if (!parsed.success) {
    return {
      status: "error",
      message:
        "Name the host, in 1 to 100 characters, and choose how long the approval lasts.",
    };
  }
  const prepared = readPrepared(String(formData.get("prepared") ?? ""));
  if (prepared === null) {
    return { status: "error", message: REFUSAL_COPY.preparedUnreadable };
  }
  const { org, hostName, placement, hours, ...replaces } = parsed.data;

  const approval: components["schemas"]["AgentEnrollmentApproval"] = {
    placement,
    hostName,
    expiresAt: new Date(Date.now() + hours * 3_600_000).toISOString(),
    ...prepared,
    ...(replaces.replacesAgent &&
    replaces.replacesAssignee &&
    replaces.replacesEpoch
      ? {
          replaces: {
            agentGrn: replaces.replacesAgent,
            assigneeGrn: replaces.replacesAssignee,
            epoch: replaces.replacesEpoch,
          },
        }
      : {}),
  };
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/agent-enrollments`,
      z.unknown(),
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(approval),
      },
    );
  } catch (error) {
    return refusal(error, "approveAgentEnrollment");
  }

  revalidatePath(`/console/orgs/${org}/hosts`);
  return { status: "approved", hostName };
}
