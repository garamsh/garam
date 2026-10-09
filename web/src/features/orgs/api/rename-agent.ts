"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { parseAgentName } from "@/lib/agent-definition";
import { garamFetchAuthenticated, refusal } from "@/lib/garam";
import type { components } from "@/types/api";

import { AgentDefinitionParser } from "./orgs";

/** What the rename form has to show: nothing yet, a refusal, or saved. */
export type RenameAgentState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "saved"; name: string };

const Schema = z.object({
  org: z.string().min(1),
  agent: z.string().min(1),
  name: z.string(),
});

/**
 * Renames a defined agent. Only the name moves: the identifier, the
 * operator and the values stay, and no operator is told (ADR-0075).
 */
export async function renameAgent(
  _state: RenameAgentState,
  formData: FormData,
): Promise<RenameAgentState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    agent: formData.get("agent"),
    name: formData.get("name"),
  });
  if (!parsed.success) {
    return { status: "error", message: "The form did not arrive complete." };
  }
  const name = parseAgentName(parsed.data.name);
  if (name.status === "error") return name;

  const { org, agent } = parsed.data;
  let saved: string;
  try {
    ({ name: saved } = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/agent-definitions/${encodeURIComponent(agent)}`,
      AgentDefinitionParser,
      {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          name: name.value,
        } satisfies components["schemas"]["AgentDefinitionPatch"]),
      },
    ));
  } catch (error) {
    return refusal(error);
  }

  revalidatePath(`/console/orgs/${org}/agents`, "layout");
  return { status: "saved", name: saved };
}
