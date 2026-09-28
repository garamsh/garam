"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { z } from "zod";

import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
} from "@/lib/garam";

/**
 * What the delete control has to show: nothing yet, or a refusal. A
 * deleted template is the list it is sent back to.
 */
export type DeleteAgentTemplateState =
  { status: "idle" } | { status: "error"; message: string };

const Schema = z.object({
  org: z.string().min(1),
  template: z.string().min(1),
});

/**
 * Deletes a template of the organization. Agents defined from it keep
 * what they copied (ADR-0075).
 */
export async function deleteAgentTemplate(
  _state: DeleteAgentTemplateState,
  formData: FormData,
): Promise<DeleteAgentTemplateState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    template: formData.get("template"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Name the template to delete." };
  }

  const { org, template } = parsed.data;
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/agent-templates/${encodeURIComponent(template)}`,
      z.unknown(),
      { method: "DELETE" },
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

  revalidatePath(`/console/orgs/${org}/agents/templates`, "layout");
  redirect(`/console/orgs/${org}/agents/templates`);
}
