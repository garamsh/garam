"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";
import type { components } from "@garam/platform-sdk-ts/api";

import { CreatedInstanceParser } from "./apps";

/**
 * What the create form has to show: nothing yet, a refusal, or the
 * instance with the registration token garam answered once. The token
 * is carried in this state because this answer is the only place it
 * exists: garam keeps its hash alone. It goes nowhere else, and the
 * client holds it until the person has kept it.
 */
export type CreateInstanceState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | {
      status: "created";
      name: string;
      registrationToken: string;
      expiresAt: string;
    };

const Schema = z.object({
  org: z.string().min(1),
  installation: z.string().trim().min(1),
  name: z.string().trim().max(100),
  description: z.string().trim().max(500),
  kind: z.string().trim().max(100),
});

/**
 * Records that the installation has an instance and answers its
 * one-time registration token (ADR-0192 decision 1). The kind is what
 * the person says the instance runs on, sent as it was typed: garam
 * stores it and never reads it. A field left empty is left out, so the
 * instance is called by its identifier.
 */
export async function createInstance(
  _state: CreateInstanceState,
  formData: FormData,
): Promise<CreateInstanceState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    installation: formData.get("installation"),
    name: formData.get("name") ?? "",
    description: formData.get("description") ?? "",
    kind: formData.get("kind") ?? "",
  });
  if (!parsed.success) {
    return {
      status: "error",
      message:
        "The name, the description or the kind is too long, or the form did not arrive complete.",
    };
  }

  const { org, installation, name, description, kind } = parsed.data;
  try {
    const created = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/installations/${encodeURIComponent(installation)}/instances`,
      CreatedInstanceParser,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          name: name || undefined,
          description: description || undefined,
          kind: kind || undefined,
        } satisfies components["schemas"]["InstanceCreation"]),
      },
    );
    revalidatePath(`/console/orgs/${org}/apps/${installation}`, "layout");
    return {
      status: "created",
      name: created.instance.name,
      registrationToken: created.registration.registrationToken,
      expiresAt: created.registration.expiresAt,
    };
  } catch (error) {
    return refusal(error);
  }
}
