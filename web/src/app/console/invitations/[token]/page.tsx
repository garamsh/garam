import type { Metadata } from "next";

import { PageHeader } from "../../_components/page-header";
import { AcceptInvitationForm } from "./_components/accept-invitation-form";

/** Head metadata for the invitation route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Accept invitation — garam" };

type Params = { params: Promise<{ token: string }> };

/** Where an invitation link lands. Renders inside the shell (ADR-0011). */
export default async function InvitationPage({ params }: Params) {
  const { token } = await params;

  return (
    <>
      <PageHeader
        title="Join the organization"
        description="Accept the invitation to become a member. Invitations work once and expire."
      />
      <AcceptInvitationForm token={token} />
    </>
  );
}
