import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { Separator } from "@/components/ui/separator";
import { getSession } from "@/features/auth/api/get-session";
import {
  getOrg,
  listGroups,
  listOrgMembers,
  listProjects,
} from "@/features/orgs/api/orgs";
import { GaramApiError } from "@/lib/garam";

import { InviteMemberForm } from "./_components/invite-member-form";
import { MemberTable } from "./_components/member-table";
import { ResourceList } from "./_components/resource-list";

type Params = { params: Promise<{ org: string }> };

export async function generateMetadata({ params }: Params): Promise<Metadata> {
  const { org } = await params;
  return { title: `${org} — garam` };
}

export default async function OrgPage({ params }: Params) {
  const { org: slug } = await params;

  // A denied read and a missing org are the same answer to a visitor:
  // the organization is not theirs to see.
  const org = await getOrg(slug).catch((error: unknown) => {
    if (
      error instanceof GaramApiError &&
      (error.status === 403 || error.status === 404)
    ) {
      notFound();
    }
    throw error;
  });

  const [groups, projects, members, session] = await Promise.all([
    listGroups(slug),
    listProjects(slug),
    listOrgMembers(slug),
    getSession(),
  ]);

  // Only owners and admins may create invitations.
  const callerRole = members.find(
    (member) => member.userGrn === session?.grn,
  )?.role;
  const canInvite = callerRole === "owner" || callerRole === "admin";

  return (
    <div className="flex flex-col gap-8">
      <div className="flex flex-col gap-1">
        <h1 className="font-heading text-2xl font-semibold">{org.name}</h1>
        <p className="font-mono text-xs text-muted-foreground">{org.grn}</p>
      </div>

      <div className="grid gap-8 md:grid-cols-2">
        <ResourceList
          title="Groups"
          emptyMessage="This organization has no groups."
          resources={groups}
        />
        <ResourceList
          title="Projects"
          emptyMessage="This organization has no projects."
          resources={projects}
        />
      </div>

      <Separator />

      <MemberTable members={members} />

      {canInvite && (
        <>
          <Separator />
          <InviteMemberForm org={slug} />
        </>
      )}
    </div>
  );
}
