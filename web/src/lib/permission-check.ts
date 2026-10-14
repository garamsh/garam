import "server-only";

import { z } from "zod";

import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
  parserFor,
} from "@/lib/garam";
import type { components } from "@/types/api";

/**
 * One question for the server: an action, and the resource it would be
 * taken on, named by its kind and identifier. No resource is the
 * organization itself.
 */
export type PermissionCheck = components["schemas"]["PermissionCheck"];

/**
 * What the server said about one action. `unknown` is a check that
 * could not be asked; a page renders the action then and lets the
 * server refuse it, as it would with no check at all. The server
 * enforces every write either way.
 */
export type Permission = "allowed" | "denied" | "unknown";

/** What a page asked, answered by the names it gave each action. */
export type PermissionMap<K extends string> = {
  get(name: K): Permission;
};

/** The most pairs `api/openapi.yaml` lets one check ask. */
export const MAX_PERMISSION_CHECKS = 50;

type PermissionCheckAnswer = components["schemas"]["PermissionCheckAnswer"];

const PermissionResourceParser = z.object({
  type: z.enum(["group", "project", "agent", "agent-template", "policy"]),
  id: z.string(),
});

/**
 * Asks the server, in one request, which of the actions a page offers
 * the signed-in caller may take in `org`. A page calls it once with
 * every action it offers, each under a name of its choosing.
 */
export async function checkPermissions<K extends string>(
  org: string,
  checks: Record<K, PermissionCheck>,
): Promise<PermissionMap<K>> {
  const asked = Object.entries<PermissionCheck>(checks);
  if (asked.length > MAX_PERMISSION_CHECKS) {
    throw new Error(
      `a permission check asks at most ${MAX_PERMISSION_CHECKS} actions, and this one asked ${asked.length}`,
    );
  }
  const answered = new Map<string, Permission>();
  const permissions = {
    get: (name: K) => answered.get(name) ?? "unknown",
  };
  if (asked.length === 0) return permissions;

  try {
    const { results } = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/permission-checks`,
      parserFor<PermissionCheckAnswer>()(
        z
          .object({
            results: z.array(
              z.object({
                action: z.string(),
                resource: PermissionResourceParser.optional(),
                allowed: z.boolean(),
              }),
            ),
          })
          // One answer per pair, in the order asked: a count that
          // differs leaves no answer to pair a name with.
          .refine((body) => body.results.length === asked.length),
      ),
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          checks: asked.map(([, check]) => check),
        } satisfies components["schemas"]["PermissionCheckRequest"]),
      },
    );
    results.forEach((result, index) => {
      answered.set(asked[index][0], result.allowed ? "allowed" : "denied");
    });
  } catch (error) {
    if (
      !(error instanceof GaramUnreachableError) &&
      !(error instanceof GaramApiError)
    ) {
      throw error;
    }
  }
  return permissions;
}
