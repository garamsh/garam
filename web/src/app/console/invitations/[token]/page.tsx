import type { Metadata } from "next";

import { AcceptInvitationForm } from "./_components/accept-invitation-form";

export const metadata: Metadata = { title: "Accept invitation — garam" };

type Params = { params: Promise<{ token: string }> };

export default async function InvitationPage({ params }: Params) {
  const { token } = await params;

  return (
    <section className="flex flex-col gap-6">
      <div className="flex flex-col gap-1">
        <h1 className="font-heading text-2xl font-semibold">
          Join the organization
        </h1>
        <p className="text-sm text-muted-foreground">
          Accept the invitation to become a member. Invitations work once
          and expire.
        </p>
      </div>
      <AcceptInvitationForm token={token} />
    </section>
  );
}
