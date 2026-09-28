"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
} from "@/lib/garam";
import type { components } from "@/types/api";

import { slugFromGrn } from "../grn";
import {
  readTemplateForm,
  type TemplateFormState,
} from "./agent-template-form";
import { AgentTemplateParser, type AgentTemplate } from "./agent-templates";

/** What the template form shows while creating (`TemplateFormState`). */
export type CreateAgentTemplateState = TemplateFormState;

/**
 * Records a template of the organization and opens its page. It
 * defines no agent: a definition copies a template's values once, when
 * it is written (ADR-0075).
 */
export async function createAgentTemplate(
  _state: CreateAgentTemplateState,
  formData: FormData,
): Promise<CreateAgentTemplateState> {
  const form = readTemplateForm(formData);
  if (form.status === "error") return form;

  const { org, fields } = form.value;
  let created: AgentTemplate;
  try {
    created = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/agent-templates`,
      AgentTemplateParser,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(
          fields satisfies components["schemas"]["AgentTemplateWrite"],
        ),
      },
    );
  } catch (error) {
    // A refusal or an outage is a state the form renders, not a throw:
    // the boundary would replace the form and everything typed into it.
    if (
      error instanceof GaramApiError ||
      error instanceof GaramUnreachableError
    ) {
      return { status: "error", message: error.message };
    }
    throw error;
  }

  revalidatePath(`/console/orgs/${org}/agents/templates`);
  redirect(`/console/orgs/${org}/agents/templates/${slugFromGrn(created.grn)}`);
}
