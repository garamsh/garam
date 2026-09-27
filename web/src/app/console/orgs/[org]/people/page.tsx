import type { Metadata } from "next";
import { UserRound } from "lucide-react";

import { Separator } from "@/components/ui/separator";
import { listOrgMembers } from "@/features/orgs";

import { ApiUnreachable } from "../../../_components/api-unreachable";
import { EmptyState } from "../../../_components/empty-state";
import { PageHeader } from "../../../_components/page-header";
import { InviteMemberForm } from "./_components/invite-member-form";
import { MemberTable } from "./_components/member-table";

/** Head metadata for the people route; Next.js reads it by name. */
export const metadata: Metadata = { title: "People — garam" };

type Params = { params: Promise<{ org: string }> };

/**
 * The people who belong to the organization, and the form that invites
 * one more. Agents are the Agents section's; a team holds both.
 *
 * Inviting is granted by `org:invite`, which owners and admins hold and
 * members do not, while every role reads the list. The form is rendered
 * for all of them and the API refuses a caller that holds only the read
 * — the same shape the operators and agents sections take. A role is
 * not the whole of the answer either: a policy grants an action and not
 * a role (ADR-0002), so only the engine knows who may invite.
 */
export default async function PeoplePage({ params }: Params) {
  const { org } = await params;
  const members = await listOrgMembers(org);

  return (
    <>
      <PageHeader
        title="People"
        description="The people who belong to this organization."
      />
      {members.status === "unreachable" ? (
        <ApiUnreachable subject="this organization's members" />
      ) : members.data.length === 0 ? (
        <EmptyState
          icon={UserRound}
          title="No people"
          description="No one belongs to this organization."
        />
      ) : (
        <MemberTable members={members.data} />
      )}
      <Separator />
      <InviteMemberForm org={org} />
    </>
  );
}
