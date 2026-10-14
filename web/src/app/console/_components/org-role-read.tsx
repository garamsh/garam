// Next.js exports no public test for its redirect throw; this is the
// module `redirect()` itself builds the error from.
import { isRedirectError } from "next/dist/client/components/redirect-error";

import { getSession } from "@/features/auth";
import { lookupOrgMembers } from "@/features/orgs";

import { ReportOrgRole, type OrgRole } from "./org-role";

/**
 * Finds the caller among the organization's members and reports the
 * role it holds there. The role is a label in the switcher and no page
 * waits on it, so any failure of the read — a refusal, a missing
 * organization, a server error, an API that did not answer, a body
 * outside the specification — reports nothing rather than reaching an
 * error or not-found boundary. The one throw let through is the
 * redirect to sign-in, which is the session ending, not the read.
 */
export async function OrgRoleRead({ org }: { org: Promise<string> }) {
  const slug = await org;
  const role = await readRole(slug);
  return role ? <ReportOrgRole org={slug} role={role} /> : null;
}

async function readRole(slug: string): Promise<OrgRole | null> {
  try {
    const [members, session] = await Promise.all([
      lookupOrgMembers(slug),
      getSession(),
    ]);
    if (members.status !== "ok" || session.status !== "signed-in") {
      return null;
    }
    const me = members.data.find(
      (member) => member.userGrn === session.user.grn,
    );
    return me?.role ?? null;
  } catch (error) {
    if (isRedirectError(error)) throw error;
    return null;
  }
}
