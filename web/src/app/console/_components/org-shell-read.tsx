// Next.js exports no public test for its redirect throw; this is the
// module `redirect()` itself builds the error from.
import { isRedirectError } from "next/dist/client/components/redirect-error";

import { getSession } from "@/features/auth";
import { lookupOrgMembers } from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";

import { CONSOLE_NAV_ENTRIES } from "./navigation";
import { ReportOrgShell } from "./org-shell";

/** What a read answered, or null where it failed in any way. */
async function settle<T>(read: Promise<T>): Promise<T | null> {
  try {
    return await read;
  } catch (error) {
    if (isRedirectError(error)) throw error;
    return null;
  }
}

/**
 * Reads what the shell above the organization's pages needs and
 * reports it: the caller's role for the switcher, and which sidebar
 * entries the server denies, asked in one check. The palette searches
 * by name when something is typed (`searchOrgObjects`), so no object
 * list is read here. Nothing here is a page's, so no failure reaches an
 * error or not-found boundary: a read that fails leaves its part empty,
 * and an unanswered permission check leaves every entry listed. The
 * one throw let through is the redirect to sign-in, which is the
 * session ending.
 */
export async function OrgShellRead({ org }: { org: Promise<string> }) {
  const slug = await org;
  const gated = CONSOLE_NAV_ENTRIES.filter((entry) => entry.read);
  const [session, members, may] = await Promise.all([
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
  ]);

  const me =
    session?.status === "signed-in" && members?.status === "ok"
      ? members.data.find((member) => member.userGrn === session.user.grn)
      : undefined;
  const hidden = gated
    .filter((entry) => may?.get(entry.key) === "denied")
    .map((entry) => entry.key);

  return <ReportOrgShell org={slug} role={me?.role ?? null} hidden={hidden} />;
}
