import { getSession } from "@/features/auth";
import { lookupOrgMembers } from "@/features/orgs";
import { GaramContractError } from "@/lib/garam";

import { ReportOrgRole } from "./org-role";

/**
 * Finds the caller among the organization's members and reports the
 * role it holds there. A refused list, an API that did not answer, or a
 * body outside the specification reports nothing: the role is a label
 * in the switcher, and no page waits on it.
 */
export async function OrgRoleRead({ org }: { org: Promise<string> }) {
  const slug = await org;
  let members: Awaited<ReturnType<typeof lookupOrgMembers>>;
  let session: Awaited<ReturnType<typeof getSession>>;
  try {
    [members, session] = await Promise.all([
      lookupOrgMembers(slug),
      getSession(),
    ]);
  } catch (error) {
    if (error instanceof GaramContractError) return null;
    throw error;
  }
  if (members.status !== "ok" || session.status !== "signed-in") return null;
  const me = members.data.find((member) => member.userGrn === session.user.grn);
  return me ? <ReportOrgRole org={slug} role={me.role} /> : null;
}
