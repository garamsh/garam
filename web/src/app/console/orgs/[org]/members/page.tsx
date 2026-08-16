import type { Metadata } from "next";
import { UserRound } from "lucide-react";

import { Separator } from "@/components/ui/separator";
import { getSession } from "@/features/auth/api/get-session";
import { listOrgMembers, orgNotFound } from "@/features/orgs/api/orgs";

import { EmptyState } from "../../../_components/empty-state";
import { PageHeader } from "../../../_components/page-header";
import { InviteMemberForm } from "./_components/invite-member-form";
import { MemberTable } from "./_components/member-table";

export const metadata: Metadata = { title: "Members — garam" };

type Params = { params: Promise<{ org: string }> };

export default async function MembersPage({ params }: Params) {
  const { org } = await params;
  const [members, session] = await Promise.all([
    listOrgMembers(org).catch(orgNotFound),
    getSession(),
  ]);

  const callerRole = members.find(
    (member) => member.userGrn === session?.grn,
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
