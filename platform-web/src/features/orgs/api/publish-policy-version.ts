"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";
import { parsePolicyDocument } from "@/lib/policy-document";
import type { PolicyDocument } from "@/lib/policy-document";
import type { components } from "@garam/platform-sdk-ts/api";

import { documentRefusal, type DocumentRefusal } from "./document-refusal";
import { PolicyParser, PolicyVersionParser } from "./policies";
import { checkOrgWideScope } from "./policy-scope";

/**
 * What the publish control has to show: nothing yet; a refusal, which
 * names the document where the engine refused it; that the policy is
 * active and the organization-wide confirmation is missing; the version
 * published; or the version saved while making it the default was
 * refused, so the policy still evaluates the one it did.
 */
export type PublishPolicyVersionState =
  | { status: "idle" }
  | DocumentRefusal
  | { status: "confirm-org-wide" }
  | { status: "confirm-unknown-scope" }
  | { status: "published"; version: number }
  | { status: "saved-not-default"; version: number; message: string };

const Schema = z.object({
  org: z.string().min(1),
  policy: z.string().min(1),
  document: z.string(),
  orgWide: z.literal("confirmed").nullable(),
});

/**
 * Publishes a document as the policy's next version: garam appends it,
 * then makes it the default the policy evaluates. These are garam's two
 * operations, the version write and its rollback, which takes any
 * stored version (ADR-0073); nothing joins them, so a refused second
 * step leaves the version saved and says so. While the policy is
 * active, publishing applies at once, and with no attachment — or
 * attachments garam will not list — it waits for the form to confirm
 * the whole organization, as making a version the default does.
 */
export async function publishPolicyVersion(
  _state: PublishPolicyVersionState,
  formData: FormData,
): Promise<PublishPolicyVersionState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    policy: formData.get("policy"),
    document: formData.get("document"),
    orgWide: formData.get("orgWide"),
  });
  if (!parsed.success) {
    return { status: "error", message: "The form did not arrive complete." };
  }
  const document = parsePolicyDocument(parsed.data.document);
  if (document.status === "error") return { ...document, field: "document" };

  const { org, policy, orgWide } = parsed.data;
  const scope = await checkOrgWideScope(org, policy, {
    confirmed: orgWide === "confirmed",
    onlyIfActive: true,
  });
  if (scope.status === "error") return scope;
  if (scope.status !== "proceed") {
    revalidatePath(`/console/orgs/${org}/permissions`, "layout");
    return scope;
  }

  const base = `/orgs/${encodeURIComponent(org)}/policies/${encodeURIComponent(policy)}`;
  let version: number;
  try {
    ({ version } = await garamFetchAuthenticated(
      `${base}/versions`,
      PolicyVersionParser,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          document: document.value,
        } satisfies { document: PolicyDocument }),
      },
    ));
  } catch (error) {
    return documentRefusal(error, "addPolicyVersion");
  }

  try {
    await garamFetchAuthenticated(`${base}/rollback`, PolicyParser, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        version,
      } satisfies components["schemas"]["PolicyRollback"]),
    });
  } catch (error) {
    revalidatePath(`/console/orgs/${org}/permissions`, "layout");
    return {
      status: "saved-not-default",
      version,
      message: refusal(error).message,
    };
  }

  revalidatePath(`/console/orgs/${org}/permissions`, "layout");
  return { status: "published", version };
}
