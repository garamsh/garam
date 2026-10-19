import "server-only";

import { z } from "zod";

import {
  GaramApiError,
  GaramContractError,
  GaramUnreachableError,
  garamFetchAuthenticated,
  parserFor,
} from "@/lib/garam";
import type { PermissionCheck } from "@/lib/permission-check";
import type { components } from "@/types/api";

/** Why the caller may or may not take one action, as garam explains it. */
export type PermissionExplanation =
  components["schemas"]["PermissionExplanation"];

/** One statement that decided, and the path it reaches the caller by. */
export type ExplainedStatement = components["schemas"]["ExplainedStatement"];

/** A member or agent the policy engine allows an action. */
export type PermissionHolder = components["schemas"]["PermissionHolder"];

const ResourceParser = z.object({
  type: z.enum(["group", "project", "agent", "policy"]),
  id: z.string(),
});

const ExplanationParser = parserFor<PermissionExplanation>()(
  z.object({
    action: z.string(),
    resource: ResourceParser.optional(),
    decision: z.enum(["allow", "deny", "unknown"]),
    basis: z.enum(["statements", "operation", "undefined-action"]),
    statements: z.array(
      z.object({
        effect: z.enum(["Allow", "Deny"]),
        source: z.enum([
          "org-role",
          "direct-role",
          "group-role",
          "project-role",
          "policy",
          "unreadable-policy",
        ]),
        sid: z.string().optional(),
        roles: z.array(z.string()).optional(),
        group: z.string().optional(),
        project: z.string().optional(),
        policy: z.string().optional(),
        attachment: z.enum(["organization", "caller", "group"]).optional(),
      }),
    ),
  }),
);

const HoldersParser = parserFor<components["schemas"]["PermissionHolders"]>()(
  z.object({
    holders: z.array(
      z.object({
        type: z.enum(["user", "agent"]),
        grn: z.string(),
        nickname: z.string().optional(),
      }),
    ),
    nextOffset: z.number().int().optional(),
  }),
);

/** The query a pair is asked by, as both operations name it. */
function pairQuery(check: PermissionCheck): URLSearchParams {
  const query = new URLSearchParams({ action: check.action });
  if (check.resource) {
    query.set("resourceType", check.resource.type);
    query.set("resourceId", check.resource.id);
  }
  return query;
}

/**
 * Whether a failed read is one a panel says nothing about rather than a
 * bug: garam refusing, not answering, or answering outside its
 * contract, which is logged.
 */
function unanswered(error: unknown): boolean {
  if (error instanceof GaramContractError) {
    console.error(error);
    return true;
  }
  return (
    error instanceof GaramUnreachableError || error instanceof GaramApiError
  );
}

/**
 * Asks garam why the signed-in caller may or may not take one action,
 * or null where it did not answer. A page asks one per action it
 * explains; the operation answers no more than one.
 */
export async function explainPermission(
  org: string,
  check: PermissionCheck,
): Promise<PermissionExplanation | null> {
  try {
    return await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/permission-explanation?${pairQuery(check)}`,
      ExplanationParser,
    );
  } catch (error) {
    if (unanswered(error)) return null;
    throw error;
  }
}

/**
 * Who among the organization's first fifty candidates the engine allows
 * an action: `forbidden` where garam refuses the caller the list, and
 * null where it did not answer. `more` says candidates past the first
 * fifty were not asked.
 */
export async function listPermissionHolders(
  org: string,
  check: PermissionCheck,
): Promise<
  | { status: "ok"; holders: PermissionHolder[]; more: boolean }
  | { status: "forbidden" }
  | null
> {
  try {
    const answer = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/permission-holders?${pairQuery(check)}`,
      HoldersParser,
    );
    return {
      status: "ok",
      holders: answer.holders,
      more: answer.nextOffset !== undefined,
    };
  } catch (error) {
    if (error instanceof GaramApiError && error.status === 403) {
      return { status: "forbidden" };
    }
    if (unanswered(error)) return null;
    throw error;
  }
}
