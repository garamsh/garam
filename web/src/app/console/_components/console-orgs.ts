import { cache } from "react";

import { listOrgs, slugFromGrn } from "@/features/orgs";

import type { ConsoleOrg } from "./navigation";

/**
 * The caller's organizations, stripped to what the console shows.
 * Request-scoped: the sidebar, the breadcrumb and the picker read one
 * list between them.
 */
export const listConsoleOrgs = cache(async (): Promise<ConsoleOrg[]> => {
  const orgs = await listOrgs();
  return orgs.map((org) => ({ slug: slugFromGrn(org.grn), name: org.name }));
});
