import type { Metadata } from "next";
import Link from "next/link";

import { Button } from "@/components/ui/button";
import {
  InvitationSummary,
  getInvitationPreview,
  invitedSentence,
  unusableInvitation,
} from "@/features/orgs";

import { ApiUnreachable } from "../../_components/api-unreachable";
import { PageHeader } from "../../_components/page-header";
import { AcceptInvitationForm } from "./_components/accept-invitation-form";

/**
 * Head metadata for the invitation route; Next.js reads it by name. The
 * path is the token, so no link followed from here sends it on as a
 * referrer.
 */
export const metadata: Metadata = {
  title: "Invitation — garam",
  referrer: "no-referrer",
};

type Params = { params: Promise<{ token: string }> };

/**
 * Where an invitation link lands, inside the shell (ADR-0011). It reads
 * the invitation first and offers to accept only one that can still be
 * used, naming the organization and the role it admits to; a used, an
 * expired or an unknown one says so with no Accept button.
 */
export default async function InvitationPage({ params }: Params) {
  const { token } = await params;
  const read = await getInvitationPreview(token);

  if (read.status === "unreachable") {
    return (
      <>
        <PageHeader
          title="Invitation"
          description="An invitation adds you to an organization with a role."
        />
        <ApiUnreachable subject="this invitation" />
      </>
    );
  }

  const unusable = unusableInvitation(read);
  if (unusable !== null || read.status !== "found") {
    return (
      <>
        <PageHeader
          title={unusable?.title ?? "Invitation"}
          description={unusable?.body ?? ""}
        />
        <div>
          <Button asChild variant="outline">
            <Link href="/console/orgs">Go to your organizations</Link>
          </Button>
        </div>
      </>
    );
  }

  const { invitation } = read;
  return (
    <>
      <PageHeader
        title={`Join ${invitation.orgName}`}
        description={`${invitedSentence(invitation)} Accepting adds you to the organization with that role.`}
      />
      <div className="flex max-w-md flex-col gap-4">
        <InvitationSummary invitation={invitation} />
        <AcceptInvitationForm token={token} orgName={invitation.orgName} />
      </div>
    </>
  );
}
