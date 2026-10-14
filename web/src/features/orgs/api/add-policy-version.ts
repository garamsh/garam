"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";
import { parsePolicyDocument } from "@/lib/policy-document";
import type { PolicyDocument } from "@/lib/policy-document";

import { PolicyVersionParser, type PolicyVersion } from "./policies";

/**
 * What the new-version form has to show: nothing yet, a refusal, or
 * the version garam stored.
 */
export type AddPolicyVersionState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "saved"; version: number };

const Schema = z.object({
  org: z.string().min(1),
  policy: z.string().min(1),
  document: z.string(),
});

/**
 * Appends a version to a policy. Versions are append-only and the
 * policy goes on evaluating its default version, so saving one changes
 * no check until that version is made the default.
 */
export async function addPolicyVersion(
  _state: AddPolicyVersionState,
  formData: FormData,
): Promise<AddPolicyVersionState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    policy: formData.get("policy"),
    document: formData.get("document"),
  });
  if (!parsed.success) {
    return { status: "error", message: "The form did not arrive complete." };
  }
  const document = parsePolicyDocument(parsed.data.document);
  if (document.status === "error") return document;

  const { org, policy } = parsed.data;
  let saved: PolicyVersion;
  try {
    saved = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/policies/${encodeURIComponent(policy)}/versions`,
      PolicyVersionParser,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          document: document.value,
        } satisfies { document: PolicyDocument }),
      },
    );
  } catch (error) {
    return refusal(error, "addPolicyVersion");
  }

  revalidatePath(`/console/orgs/${org}/permissions`, "layout");
  return { status: "saved", version: saved.version };
}
