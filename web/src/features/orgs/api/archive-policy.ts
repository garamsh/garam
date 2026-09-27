"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
} from "@/lib/garam";

import { PolicyParser } from "./policies";

/** What an archive control has to show: nothing yet, or a refusal. */
export type ArchivePolicyState =
  { status: "idle" } | { status: "error"; message: string };

const Schema = z.object({
  org: z.string().min(1),
  policy: z.string().min(1),
});

/**
 * Archives a policy: it stops contributing to checks, and its versions
 * and attachments are kept. garam deletes no policy, so archiving is
 * how one retires.
 */
export async function archivePolicy(
  _state: ArchivePolicyState,
  formData: FormData,
): Promise<ArchivePolicyState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    policy: formData.get("policy"),
  });
  if (!parsed.success) {
    return { status: "error", message: "The form did not arrive complete." };
  }

  const { org, policy } = parsed.data;
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/policies/${encodeURIComponent(policy)}/archive`,
      PolicyParser,
      { method: "POST" },
    );
  } catch (error) {
    if (
      error instanceof GaramApiError ||
      error instanceof GaramUnreachableError
    ) {
      return { status: "error", message: error.message };
    }
    throw error;
  }

  revalidatePath(`/console/orgs/${org}/permissions`, "layout");
  return { status: "idle" };
}
