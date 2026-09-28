"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { parseAgentValues } from "@/lib/agent-definition";
import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
} from "@/lib/garam";
import type { components } from "@/types/api";

import { AgentDefinitionParser } from "./orgs";

/** The settings another save left in place, to recover from. */
export type SavedSettings = {
  revision: number;
  values: Record<string, string>;
};

/**
 * What the settings form has to show: nothing yet, a refusal, the
 * revision a save produced, or a conflict — another save landed after
 * the form was read. A conflict carries that save where it could be
 * read back, and null where it could not.
 */
export type ReviseAgentValuesState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "saved"; revision: number }
  | { status: "conflict"; latest: SavedSettings | null };

const Schema = z.object({
  org: z.string().min(1),
  agent: z.string().min(1),
  expectedRevision: z.coerce.number().int().min(1),
  entries: z.array(z.object({ key: z.string(), value: z.string() })),
});

/**
 * Saves an agent's settings as the next revision of the one the form
 * read (ADR-0076). Saving tells no operator anything and applies
 * nothing; where another save got there first, garam refuses this one
 * and the form is handed the saved revision to recover from.
 */
export async function reviseAgentValues(
  _state: ReviseAgentValuesState,
  formData: FormData,
): Promise<ReviseAgentValuesState> {
  const keyFields = formData.getAll("key");
  const valueFields = formData.getAll("value");
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    agent: formData.get("agent"),
    expectedRevision: formData.get("expectedRevision"),
    entries: Array.from(
      { length: Math.max(keyFields.length, valueFields.length) },
      (_, index) => ({ key: keyFields[index], value: valueFields[index] }),
    ),
  });
  if (!parsed.success) {
    return { status: "error", message: "The form did not arrive complete." };
  }
  const values = parseAgentValues(parsed.data.entries);
  if (values.status === "error") return values;

  const { org, agent, expectedRevision } = parsed.data;
  const path = `/orgs/${encodeURIComponent(org)}/agent-definitions/${encodeURIComponent(agent)}`;
  let revision: number;
  try {
    ({ revision } = await garamFetchAuthenticated(
      `${path}/values`,
      AgentDefinitionParser,
      {
        method: "PUT",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          expectedRevision,
          values: values.value,
        } satisfies components["schemas"]["AgentValuesRevision"]),
      },
    ));
  } catch (error) {
    if (error instanceof GaramApiError && error.status === 409) {
      return { status: "conflict", latest: await latestSettings(path) };
    }
    if (
      error instanceof GaramApiError ||
      error instanceof GaramUnreachableError
    ) {
      return { status: "error", message: error.message };
    }
    throw error;
  }

  revalidatePath(`/console/orgs/${org}/agents`, "layout");
  return { status: "saved", revision };
}

/**
 * The settings as saved now, or null where they could not be read: the
 * conflict stands either way, and the form says what it could learn.
 */
async function latestSettings(path: string): Promise<SavedSettings | null> {
  try {
    const { revision, values } = await garamFetchAuthenticated(
      path,
      AgentDefinitionParser,
    );
    return { revision, values };
  } catch (error) {
    if (
      error instanceof GaramApiError ||
      error instanceof GaramUnreachableError
    ) {
      return null;
    }
    throw error;
  }
}
