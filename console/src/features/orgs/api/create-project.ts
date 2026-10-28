"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";
import type { components } from "@/types/api";

import { slugFromGrn } from "@/lib/grn";
import { ProjectParser, type Project } from "./orgs";

/**
 * What a create form has to show: nothing yet, a refusal, or what garam
 * recorded. `identifier` is the segment the API's own paths address the
 * resource by, and the console shows names everywhere else (ADR-0011),
 * so this answer is the one place a person reads it.
 */
export type CreateProjectState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "created"; name: string; identifier: string };

const Schema = z.object({
  org: z.string().min(1),
  name: z.string().trim().min(1),
});

/**
 * Creates a project of the organization. An agent's GRN names the
 * project it was minted in (`docs/architecture/grn-model.md` §Format),
 * so an organization holding none cannot reach that path from here.
 *
 * The name is the only field sent: `roles` is optional on the operation
 * and nothing here chooses one.
 */
export async function createProject(
  _state: CreateProjectState,
  formData: FormData,
): Promise<CreateProjectState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    name: formData.get("name"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Give the project a name." };
  }

  const { org, name } = parsed.data;
  let created: Project;
  try {
    created = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/projects`,
      ProjectParser,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          name,
        } satisfies components["schemas"]["ProjectCreate"]),
      },
    );
  } catch (error) {
    // An unreachable API is a state the form renders, not a throw: the
    // boundary would replace the form and the name typed into it.
    return refusal(error);
  }

  revalidatePath(`/console/orgs/${org}/projects`);
  return {
    status: "created",
    name: created.name,
    identifier: slugFromGrn(created.grn),
  };
}
