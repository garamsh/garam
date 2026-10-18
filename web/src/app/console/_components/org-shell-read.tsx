// Next.js exports no public test for its redirect throw; this is the
// module `redirect()` itself builds the error from.
import { isRedirectError } from "next/dist/client/components/redirect-error";

import { getSession } from "@/features/auth";
import {
  UNNAMED_AGENT,
  directoryAgents,
  listPolicies,
  lookupAgentDefinitions,
  lookupAgents,
  lookupGroups,
  lookupOrgMembers,
  lookupProjects,
  slugFromGrn,
  type Catalog,
} from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";

import { CONSOLE_NAV_ENTRIES } from "./navigation";
import { ReportOrgShell, type PaletteObject } from "./org-shell";

/** What a read answered, or null where it failed in any way. */
async function settle<T>(read: Promise<T>): Promise<T | null> {
  try {
    return await read;
  } catch (error) {
    if (isRedirectError(error)) throw error;
    return null;
  }
}

/** A read's entries, or none where it did not answer them. */
function entries<T>(
  read: { status: string; data?: readonly T[] } | null,
): readonly T[] {
  return read?.status === "ok" ? (read.data ?? []) : [];
}

/** A read as a catalog: one that did not answer lists no one. */
function catalog<T>(
  read: { status: "ok"; data: readonly T[] } | { status: string } | null,
): Catalog<T> {
  return read !== null && read.status === "ok" && "data" in read
    ? { status: "ok", data: read.data }
    : { status: "forbidden" };
}

/**
 * Reads what the shell above the organization's pages needs and
 * reports it: the caller's role for the switcher, which sidebar entries
 * the server denies, asked in one check, and what the palette finds by
 * name. Nothing here is a page's, so no failure reaches an error or
 * not-found boundary: a read that fails leaves its part empty, and an
 * unanswered permission check leaves every entry listed. The one throw
 * let through is the redirect to sign-in, which is the session ending.
 */
export async function OrgShellRead({ org }: { org: Promise<string> }) {
  const slug = await org;
  const gated = CONSOLE_NAV_ENTRIES.filter((entry) => entry.read);
  const [
    session,
    members,
    may,
    groups,
    agents,
    definitions,
    policies,
    projects,
  ] = await Promise.all([
    settle(getSession()),
    settle(lookupOrgMembers(slug)),
    settle(
      checkPermissions(
        slug,
        Object.fromEntries(
          gated.map((entry) => [entry.key, { action: entry.read ?? "" }]),
        ),
      ),
    ),
    settle(lookupGroups(slug)),
    settle(lookupAgents(slug)),
    settle(lookupAgentDefinitions(slug)),
    settle(listPolicies(slug)),
    settle(lookupProjects(slug)),
  ]);

  const me =
    session?.status === "signed-in"
      ? entries(members).find((member) => member.userGrn === session.user.grn)
      : undefined;
  const hidden = gated
    .filter((entry) => may?.get(entry.key) === "denied")
    .map((entry) => entry.key);

  const objects: PaletteObject[] = [
    ...entries(members).map((member) => ({
      kind: "person" as const,
      name: member.nickname ?? "No name on record",
      identifier: slugFromGrn(member.userGrn),
      // A person has no page of their own yet.
      segment: "people",
    })),
    ...directoryAgents({
      agents: catalog(agents),
      definitions: catalog(definitions),
    }).map((agent) => ({
      kind: "agent" as const,
      name: agent.name ?? UNNAMED_AGENT,
      identifier: agent.identifier,
      segment: `agents/${agent.identifier}`,
    })),
    ...entries(groups).map((group) => ({
      kind: "team" as const,
      name: group.name,
      identifier: slugFromGrn(group.grn),
      segment: `teams/${slugFromGrn(group.grn)}`,
    })),
    ...entries(policies).map((policy) => ({
      kind: "policy" as const,
      name: policy.name,
      identifier: slugFromGrn(policy.grn),
      segment: `permissions/${slugFromGrn(policy.grn)}`,
    })),
    ...entries(projects).map((project) => ({
      kind: "project" as const,
      name: project.name,
      identifier: slugFromGrn(project.grn),
      // A project has no page of its own yet.
      segment: "projects",
    })),
  ];

  return (
    <ReportOrgShell
      org={slug}
      role={me?.role ?? null}
      hidden={hidden}
      objects={objects}
    />
  );
}
