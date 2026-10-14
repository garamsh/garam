"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { z } from "zod";

import { garamFetchAuthenticated } from "@/lib/garam";
import { parsePolicyDocument } from "@/lib/policy-document";
import type { PolicyDocument } from "@/lib/policy-document";
import type { components } from "@/types/api";

import { slugFromGrn } from "../grn";
import { documentRefusal, type DocumentRefusal } from "./document-refusal";
import { PolicyParser, type Policy } from "./policies";

/**
 * What the create form has to show: nothing yet, or a refusal. There is
 * no created state — a created policy is the page it is sent to.
 */
export type CreatePolicyState =
  | { status: "idle" }
  | DocumentRefusal
  | { status: "error"; message: string; field: "name" };

const Schema = z.object({
  org: z.string().min(1),
  name: z.string().trim().min(1),
  document: z.string(),
});

/**
 * Creates a policy of the organization from a name and its first
 * document, and opens the page of the one garam recorded. garam stores
 * it as a draft, which no check evaluates, so creating one grants
 * nothing.
 */
export async function createPolicy(
  _state: CreatePolicyState,
  formData: FormData,
): Promise<CreatePolicyState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    name: formData.get("name"),
    document: formData.get("document"),
  });
  if (!parsed.success) {
    return {
      status: "error",
      message: "Give the policy a name.",
      field: "name",
    };
  }
  const document = parsePolicyDocument(parsed.data.document);
  if (document.status === "error") return { ...document, field: "document" };

  const { org, name } = parsed.data;
  let created: Policy;
  try {
    created = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/policies`,
      PolicyParser,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          name,
          document: document.value,
        } satisfies Omit<components["schemas"]["PolicyCreate"], "document"> & {
          document: PolicyDocument;
        }),
      },
    );
  } catch (error) {
    // A refused document, a refusal or an outage is a state the form
    // renders, not a throw: the boundary would replace the form and
    // the document typed into it.
    return documentRefusal(error, "createPolicy");
  }

  revalidatePath(`/console/orgs/${org}/permissions`, "layout");
  redirect(`/console/orgs/${org}/permissions/${slugFromGrn(created.grn)}`);
}
