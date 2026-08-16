"use client";

import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  createInvitation,
  type CreateInvitationState,
} from "@/features/orgs/actions";

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
          <Label htmlFor="role">Role</Label>
          <Select name="role" defaultValue="member">
            <SelectTrigger id="role" className="w-40">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="member">Member</SelectItem>
              <SelectItem value="admin">Admin</SelectItem>
            </SelectContent>
          </Select>
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
            <Input
              readOnly
              value={invitationUrl}
              aria-label="Invitation link"
              onFocus={(event) => event.target.select()}
              className="min-w-0 flex-1 bg-muted font-mono text-xs"
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
