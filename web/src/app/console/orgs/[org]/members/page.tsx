import type { Metadata } from "next";
import { UserRound } from "lucide-react";

import { Separator } from "@/components/ui/separator";
import { getSession } from "@/features/auth";
import { listOrgMembers, orgNotFound } from "@/features/orgs";

import { EmptyState } from "../../../_components/empty-state";
import { PageHeader } from "../../../_components/page-header";
import { InviteMemberForm } from "./_components/invite-member-form";
import { MemberTable } from "./_components/member-table";

/** Head metadata for the members route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Members — garam" };

type Params = { params: Promise<{ org: string }> };

/**
 * The organization's members. The invite form appears only for a
 * caller whose own membership carries a role that may invite.
 */
export default async function MembersPage({ params }: Params) {
  const { org } = await params;
  const [members, session] = await Promise.all([
    listOrgMembers(org).catch(orgNotFound),
    getSession(),
  ]);

  const callerGrn = session.status === "signed-in" ? session.user.grn : null;
  const callerRole = members.find(
    (member) => member.userGrn === callerGrn,
  )?.role;
  const canInvite = callerRole === "owner" || callerRole === "admin";

  return (
    <>
      <PageHeader
        title="Members"
        description="The people who belong to this organization."
      />
      {members.length === 0 ? (
        <EmptyState
          icon={UserRound}
          title="No members"
          description="This organization has no members."
        />
      ) : (
        <MemberTable members={members} />
      )}

      {canInvite && (
        <>
          <Separator />
          <InviteMemberForm org={org} />
        </>
      )}
    </>
  );
}
