"use server";

// Next.js exports no public test for its redirect throw; this is the
// module `redirect()` itself builds the error from.
import { isRedirectError } from "next/dist/client/components/redirect-error";
import { z } from "zod";

import { slugFromGrn } from "@/lib/grn";

import { directoryAgents, UNNAMED_AGENT } from "../agent-directory";
import {
  lookupAgentDefinitions,
  lookupAgents,
  lookupGroups,
  lookupOrgMembers,
  lookupProjects,
} from "./orgs";
import { listPolicies } from "./policies";

/** What the palette finds by name. */
export type FoundKind = "person" | "agent" | "team" | "policy" | "project";

/** One thing found: what it is, its name, and the identifier it is at. */
export type FoundObject = { name: string; identifier: string };

/**
 * What a search answered: up to `PER_KIND` of each kind, in the order
 * the lists gave them, and how many more matched past that. A kind
 * whose list garam refused or did not answer finds nothing.
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

/** A read's entries, or none where it did not answer them. */
async function entries<T>(
  read: Promise<{ status: string; data?: readonly T[] }>,
): Promise<readonly T[]> {
  try {
    const answer = await read;
    return answer.status === "ok" ? (answer.data ?? []) : [];
  } catch (error) {
    // The session ending is the one failure that is not the read's.
    if (isRedirectError(error)) throw error;
    return [];
  }
}

/**
 * Finds the organization's people, agents, teams, policies and
 * projects whose name or identifier holds what was typed, case aside,
 * for the command palette (ADR-0113). It reads the lists when asked,
 * so a page the palette is never searched on reads none of them.
 * garam offers no search of its own, so each list is read whole and
 * matched here, and only the first few of each kind cross back.
 */
export async function searchOrgObjects(
  org: string,
  query: string,
): Promise<ObjectSearch> {
  const parsed = Query.safeParse({ org, query });
  if (!parsed.success) return { status: "unanswered" };
  const needle = parsed.data.query.toLowerCase();

  const [people, groups, agents, definitions, policies, projects] =
    await Promise.all([
      entries(lookupOrgMembers(org)),
      entries(lookupGroups(org)),
      entries(lookupAgents(org)),
      entries(lookupAgentDefinitions(org)),
      entries(listPolicies(org)),
      entries(lookupProjects(org)),
    ]);

  const all: Record<FoundKind, FoundObject[]> = {
    person: people.map((member) => ({
      name: member.nickname ?? "No name on record",
      identifier: slugFromGrn(member.userGrn),
    })),
    agent: directoryAgents({
      agents: { status: "ok", data: agents },
      definitions: { status: "ok", data: definitions },
    }).map((agent) => ({
      name: agent.name ?? UNNAMED_AGENT,
      identifier: agent.identifier,
    })),
    team: groups.map((group) => ({
      name: group.name,
      identifier: slugFromGrn(group.grn),
    })),
    policy: policies.map((policy) => ({
      name: policy.name,
      identifier: slugFromGrn(policy.grn),
    })),
    project: projects.map((project) => ({
      name: project.name,
      identifier: slugFromGrn(project.grn),
    })),
  };

  const found = (Object.keys(all) as FoundKind[]).map((kind) => {
    const matched = all[kind].filter(
      (object) =>
        object.name.toLowerCase().includes(needle) ||
        object.identifier.toLowerCase().includes(needle),
    );
    return {
      kind,
      objects: matched.slice(0, PER_KIND),
      more: Math.max(0, matched.length - PER_KIND),
    };
  });
  return { status: "ok", found };
}
