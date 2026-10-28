import { cache } from "react";

import { listOrgs, slugFromGrn } from "@/features/orgs";
import { GaramUnreachableError } from "@/lib/garam";

import type { ConsoleOrg } from "./navigation";

/**
 * The caller's organizations, or the fact that the API did not answer.
 * An empty list and an unanswered read are different states, and a shell
 * that cannot tell them apart reports "none" for an outage.
 */
export type ConsoleOrgs =
  { status: "ok"; orgs: ConsoleOrg[] } | { status: "unreachable" };

/**
 * The caller's organizations, stripped to what the console shows.
 * Request-scoped: the sidebar, the breadcrumb and the picker read one
 * list between them.
 */
export const listConsoleOrgs = cache(async (): Promise<ConsoleOrgs> => {
  try {
    const orgs = await listOrgs();
    return {
      status: "ok",
      orgs: orgs.map((org) => ({ slug: slugFromGrn(org.grn), name: org.name })),
    };
  } catch (error) {
    if (error instanceof GaramUnreachableError) {
      return { status: "unreachable" };
    }
    throw error;
  }
});
