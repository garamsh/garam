"use client";

import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  createInvitation,
  type CreateInvitationState,
} from "@/features/orgs/api/create-invitation";

const initialState: CreateInvitationState = { status: "idle" };

/**
 * Creates invitation links for the org. There is no email sending, so
 * the resulting link is surfaced for the owner/admin to copy.
 */
export function InviteMemberForm({ org }: { org: string }) {
  const [state, formAction, pending] = useActionState(
    createInvitation,
    initialState,
  );
  // The copied token, not a flag: a freshly created invitation is a
  // different token, so the button stops claiming it was copied.
  const [copiedToken, setCopiedToken] = useState<string | null>(null);

  const invitationUrl =
    state.status === "created"
      ? `${window.location.origin}/console/invitations/${state.token}`
      : null;
  const copied = state.status === "created" && copiedToken === state.token;

  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-semibold">Invite a member</h2>
      <form action={formAction} className="flex flex-wrap items-end gap-3">
        <input type="hidden" name="org" value={org} />
        <div className="flex flex-col gap-2">
          <label htmlFor="role" className="text-sm font-medium">
            Role
          </label>
          <select
            id="role"
            name="role"
            defaultValue="member"
            className="h-9 rounded-md border border-input bg-background px-3 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
          >
            <option value="member">Member</option>
            <option value="admin">Admin</option>
          </select>
        </div>
        <Button type="submit" disabled={pending}>
          {pending ? "Creating…" : "Create invitation"}
        </Button>
      </form>
      {state.status === "error" && (
        <p role="alert" className="text-sm text-destructive">
          {state.message}
        </p>
      )}
      {invitationUrl && (
        <div className="flex flex-col gap-2">
          <p className="text-sm text-muted-foreground">
            Share this link with the person you invited. It works once and
            then expires.
          </p>
          <div className="flex max-w-lg gap-2">
            <input
              readOnly
              value={invitationUrl}
              aria-label="Invitation link"
              onFocus={(event) => event.target.select()}
              className="h-9 min-w-0 flex-1 rounded-md border border-input bg-muted px-3 font-mono text-xs outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
            />
            <Button
              type="button"
              variant="outline"
              onClick={async () => {
                await navigator.clipboard.writeText(invitationUrl);
                setCopiedToken(state.status === "created" ? state.token : null);
              }}
            >
              {copied ? "Copied" : "Copy"}
            </Button>
          </div>
        </div>
      )}
    </section>
  );
}
