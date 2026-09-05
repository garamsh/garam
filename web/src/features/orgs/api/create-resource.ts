"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
} from "@/lib/garam";
import type { components } from "@/types/api";

import { slugFromGrn } from "../grn";
import { GroupParser, ProjectParser, type Group, type Project } from "./orgs";

/**
 * What a create form has to show: nothing yet, a refusal, or what garam
 * recorded. `identifier` is the segment the API's own paths address the
 * resource by, and the console shows names everywhere else (ADR-0011),
 * so this answer is the one place a person reads it.
 */
export type CreateResourceState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "created"; name: string; identifier: string };

/**
 * All a group and a project differ by. The API gives them one shape, so
 * `ResourceList` already renders both from one component and creating
 * one takes the same body, answer and refusals; what is left is the
 * segment the API path and the console route share, and the noun a
 * refusal names.
 */
type Resource<T> = {
  segment: "groups" | "projects";
  noun: string;
  parser: z.ZodType<T>;
};

const GROUP: Resource<Group> = {
  segment: "groups",
  noun: "group",
  parser: GroupParser,
};

const PROJECT: Resource<Project> = {
  segment: "projects",
  noun: "project",
  parser: ProjectParser,
};

/** Creates a group of the organization. */
export async function createGroup(
  _state: CreateResourceState,
  formData: FormData,
): Promise<CreateResourceState> {
  return createResource(GROUP, formData);
}

/**
 * Creates a project of the organization. An agent's GRN names the
 * project it was minted in (`docs/architecture/grn-model.md` §Format),
 * so an organization holding none cannot reach that path from here.
 */
export async function createProject(
  _state: CreateResourceState,
  formData: FormData,
): Promise<CreateResourceState> {
  return createResource(PROJECT, formData);
}

const Schema = z.object({
  org: z.string().min(1),
  name: z.string().trim().min(1),
});

/**
 * Posts one create and reads back what garam recorded. The name is the
 * only field sent: `roles` is optional on both operations and nothing
 * here chooses one.
 */
async function createResource<T extends { grn: string; name: string }>(
  resource: Resource<T>,
  formData: FormData,
): Promise<CreateResourceState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    name: formData.get("name"),
  });
  if (!parsed.success) {
    return { status: "error", message: `Give the ${resource.noun} a name.` };
  }

  const { org, name } = parsed.data;
  let created: T;
  try {
    created = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/${resource.segment}`,
      resource.parser,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        // One body reaches two operations, so it owes what both
        // describe: an intersection fails the typecheck where either
        // specification adds a field, which a union would not.
        body: JSON.stringify({
          name,
        } satisfies components["schemas"]["GroupCreate"] &
          components["schemas"]["ProjectCreate"]),
      },
    );
  } catch (error) {
    // An unreachable API is a state the form renders, not a throw: the
    // boundary would replace the form and the name typed into it.
    if (
      error instanceof GaramApiError ||
      error instanceof GaramUnreachableError
    ) {
      return { status: "error", message: error.message };
    }
    throw error;
  }

  revalidatePath(`/console/orgs/${org}/${resource.segment}`);
  return {
    status: "created",
    name: created.name,
    identifier: slugFromGrn(created.grn),
  };
}
