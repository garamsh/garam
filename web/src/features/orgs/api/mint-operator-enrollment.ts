"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated, parserFor, refusal } from "@/lib/garam";
import type { components } from "@/types/api";

type OperatorEnrollment = components["schemas"]["OperatorEnrollment"];

/**
 * What the control has to show: nothing yet, a refusal, or the token
 * garam minted. The token is carried here because this answer is the
 * only place it exists: garam stores its hash (ADR-0064).
 */
export type MintOperatorEnrollmentState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "minted"; token: string; expiresAt: string };

const Schema = z.object({
  org: z.string().min(1),
  operator: z.string().trim().min(1),
});

const OperatorEnrollmentParser = parserFor<OperatorEnrollment>()(
  z.object({ token: z.string(), expiresAt: z.string() }),
);

/**
 * Mints a fresh enrollment token for an operator holding no
 * certificate, which voids any token minted before it (ADR-0067).
 */
export async function mintOperatorEnrollment(
  _state: MintOperatorEnrollmentState,
  formData: FormData,
): Promise<MintOperatorEnrollmentState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    operator: formData.get("operator"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Name the operator to issue for." };
  }

  const { org, operator } = parsed.data;
  let minted: OperatorEnrollment;
  try {
    minted = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/operators/${encodeURIComponent(operator)}/enrollment`,
      OperatorEnrollmentParser,
      { method: "POST" },
    );
  } catch (error) {
    return refusal(error, "mintOperatorEnrollment");
  }

  revalidatePath(`/console/orgs/${org}/operators/${operator}`);
  return { status: "minted", token: minted.token, expiresAt: minted.expiresAt };
}
