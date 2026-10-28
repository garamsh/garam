"use server";

// Next.js exports no public test for its redirect throw; this is the
// module `redirect()` itself builds the error from.
import { isRedirectError } from "next/dist/client/components/redirect-error";
import { z } from "zod";

import { garamFetchAuthenticated, parserFor } from "@/lib/garam";
import { slugFromGrn } from "@/lib/grn";
import type { components } from "@garam/platform-client/api";

import { GrnParser, orgPath } from "./orgs";

/** What the palette finds by name. */
export type FoundKind = components["schemas"]["FoundKind"]["kind"];

/** One thing found: what it is, its name, and the identifier it is at. */
export type FoundObject = { name: string; identifier: string };

/**
 * What a search answered: up to `PER_KIND` of each kind garam lets the
 * caller list, in the order its list gives them, and how many more
 * matched past that.
 */
export type ObjectSearch =
  | {
      status: "ok";
      found: { kind: FoundKind; objects: FoundObject[]; more: number }[];
    }
  | { status: "unanswered" };

/** How many of one kind a search answers before counting the rest. */
const PER_KIND = 5;

const Query = z.object({
  org: z.string().min(1),
  query: z.string().trim().min(1).max(100),
});

const OrgSearchParser = parserFor<components["schemas"]["OrgSearch"]>()(
  z.object({
    found: z.array(
      z.object({
        kind: z.enum(["person", "agent", "team", "policy", "project"]),
        objects: z.array(
          z.object({ grn: GrnParser, name: z.string().optional() }),
        ),
        more: z.number().int(),
      }),
    ),
  }),
);

/**
 * Finds the organization's people, agents, teams, policies and
 * projects whose name or identifier holds what was typed, case aside,
 * for the command palette (ADR-0113). garam matches them
 * (`searchOrg`), so only the first few of each kind cross to the web
 * server, and a page the palette is never searched on asks nothing.
 */
export async function searchOrgObjects(
  org: string,
  query: string,
): Promise<ObjectSearch> {
  const parsed = Query.safeParse({ org, query });
  if (!parsed.success) return { status: "unanswered" };
  const params = new URLSearchParams({
    q: parsed.data.query,
    limit: String(PER_KIND),
  });
  try {
    const answer = await garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/search?${params}`,
      OrgSearchParser,
    );
    return {
      status: "ok",
      found: answer.found.map(({ kind, objects, more }) => ({
        kind,
        objects: objects.map((object) => ({
          // An agent the organization calls nothing is shown by its
          // identifier (ADR-0119).
          name:
            object.name ??
            (kind === "agent" ? slugFromGrn(object.grn) : "No name on record"),
          identifier: slugFromGrn(object.grn),
        })),
        more,
      })),
    };
  } catch (error) {
    // The session ending is the one failure that is not the search's.
    if (isRedirectError(error)) throw error;
    return { status: "unanswered" };
  }
}
