import { Building2 } from "lucide-react";

import type {
  InvitationPreview,
  InvitationPreviewRead,
} from "../api/invitation-preview";

/** A role as a sentence reads it, with its article: "an admin". */
export function roleWithArticle(role: InvitationPreview["role"]): string {
  return role === "admin" ? "an admin" : `a ${role}`;
}

/** The sentence a usable invitation is introduced with. */
export function invitedSentence(invitation: InvitationPreview): string {
  return `You've been invited to join ${invitation.orgName} as ${roleWithArticle(invitation.role)}.`;
}

/**
 * What a page says in place of an invitation it cannot offer to accept:
 * one already used, one past its expiry, or a token garam does not
 * hold. A usable invitation, and a read with no answer, have none.
 */
export function unusableInvitation(
  read: InvitationPreviewRead,
): { title: string; body: string } | null {
  if (read.status === "not-found") {
    return {
      title: "This invitation link isn't valid",
      body: "Check that you opened the whole link, or ask the person who invited you for a new one.",
    };
  }
  if (read.status !== "found") return null;
  const { invitation } = read;
  switch (invitation.state) {
    case "accepted":
      return {
        title: "This invitation has already been used",
        body: `An invitation to ${invitation.orgName} works once. If you haven't joined yet, ask an admin of ${invitation.orgName} for a new one.`,
      };
    case "expired":
      return {
        title: "This invitation has expired",
        body: `Ask an admin of ${invitation.orgName} to send you a new one.`,
      };
    case "pending":
      return null;
  }
}

/**
 * The organization and role a usable invitation admits to, as a panel
 * the sign-in, register and invitation pages open with. The org is named
 * and never addressed by its identifier.
 */
export function InvitationSummary({
  invitation,
}: {
  invitation: InvitationPreview;
}) {
  return (
    <div className="flex items-center gap-3 rounded-lg border border-border bg-muted/40 p-3">
      <span
        aria-hidden
        className="flex size-9 shrink-0 items-center justify-center rounded-md border border-border bg-background"
      >
        <Building2 className="size-4 text-muted-foreground" strokeWidth={1.5} />
      </span>
      <div className="flex min-w-0 flex-col">
        <span className="truncate text-sm font-medium">
          {invitation.orgName}
        </span>
        <span className="text-xs text-muted-foreground">
          Joining as {roleWithArticle(invitation.role)}
        </span>
      </div>
    </div>
  );
}
