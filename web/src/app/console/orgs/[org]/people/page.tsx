import type { Metadata } from "next";

import { getOrg, lookupOrgMembers, slugFromGrn } from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";

import { ApiUnreachable } from "../../../_components/api-unreachable";
import { RefusedRead } from "../../../_components/refused-read";
import { ListPage } from "../../../_components/templates/list-page";
import { InviteDialog } from "./_components/invite-dialog";
import { MemberTable } from "./_components/member-table";

/** Head metadata for the people route; Next.js reads it by name. */
export const metadata: Metadata = { title: "People — garam" };

type Params = { params: Promise<{ org: string }> };

const TITLE = "People";
const DESCRIPTION = "The people who belong to this organization.";

/**
 * The people who belong to the organization, and the invitation that
 * brings one more in. Agents are the Agents section's; a team holds
 * both.
 *
 * The organization is read for itself, so one the caller cannot read
 * answers 404 (ADR-0011); inside one it can read, a refused list says
 * so rather than showing no one. The page asks the server whether the
 * caller may invite, and leaves the invitation out only when the
 * answer is no: a policy grants an action and not a role (ADR-0002),
 * so only the server knows who may invite.
 */
export default async function PeoplePage({ params }: Params) {
  const { org } = await params;
  const [organization, members, permissions] = await Promise.all([
    getOrg(org),
    lookupOrgMembers(org),
    checkPermissions(org, { invite: { action: "org:invite" } }),
  ]);

  const invite = permissions.get("invite") !== "denied" && (
    <InviteDialog org={org} />
  );

  if (
    organization.status === "unreachable" ||
    members.status === "unreachable"
  ) {
    return (
      <ListPage title={TITLE} description={DESCRIPTION}>
        <ApiUnreachable subject="this organization's members" />
      </ListPage>
    );
  }
  if (members.status === "forbidden") {
    return (
      <ListPage title={TITLE} description={DESCRIPTION} primaryAction={invite}>
        <RefusedRead subject="this organization's people" />
      </ListPage>
    );
  }

  return (
    <ListPage
      title={TITLE}
      description={DESCRIPTION}
      count={members.data.length}
      primaryAction={invite}
    >
      <MemberTable
        members={members.data.map((member) => ({
          identifier: slugFromGrn(member.userGrn),
          nickname: member.nickname ?? null,
          role: member.role,
          since: member.since,
        }))}
      />
    </ListPage>
  );
}
