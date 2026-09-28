"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
} from "@/lib/garam";
import type { components } from "@/types/api";

import {
  readTemplateForm,
  type TemplateFormState,
} from "./agent-template-form";
import { AgentTemplateParser } from "./agent-templates";

/** What the template form shows while editing (`TemplateFormState`). */
export type UpdateAgentTemplateState = TemplateFormState;

const TemplateSchema = z.string().min(1);

/**
 * Replaces what a template says. Agents already defined from it keep
 * the values they copied: nothing links them to the template
 * (ADR-0075).
 */
export async function updateAgentTemplate(
  _state: UpdateAgentTemplateState,
  formData: FormData,
): Promise<UpdateAgentTemplateState> {
  const template = TemplateSchema.safeParse(formData.get("template"));
  if (!template.success) {
    return { status: "error", message: "The form did not arrive complete." };
  }
  const form = readTemplateForm(formData);
  if (form.status === "error") return form;

  const { org, fields } = form.value;
  let saved: string;
  try {
    ({ name: saved } = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/agent-templates/${encodeURIComponent(template.data)}`,
      AgentTemplateParser,
      {
        method: "PUT",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(
          fields satisfies components["schemas"]["AgentTemplateWrite"],
        ),
      },
    ));
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
  return { status: "saved", name: saved };
}
