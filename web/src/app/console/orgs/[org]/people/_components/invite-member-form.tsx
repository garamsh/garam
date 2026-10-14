"use client";

import { useActionState, useState } from "react";

import { KeptSelect } from "@/components/kept-select";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  createInvitation,
  type CreateInvitationState,
} from "@/features/orgs/actions";

const initialState: CreateInvitationState = { status: "idle" };

/** What the clipboard answered for one invitation's link. */
type CopyAttempt = { token: string; outcome: "copied" | "failed" };

/**
 * Creates invitation links for the org. There is no email sending, so
 * the resulting link is surfaced for the owner/admin to copy.
 */
export function InviteMemberForm({ org }: { org: string }) {
  const [state, formAction, pending] = useActionState(
    createInvitation,
    initialState,
  );
  // Keyed by token, not a flag: a freshly created invitation is a
  // different token, so neither the claim that it was copied nor the
  // failure the previous one reported carries over to it.
  const [attempt, setAttempt] = useState<CopyAttempt | null>(null);
  const [role, setRole] = useState("member");

  const invitation =
    state.status === "created"
      ? {
          token: state.token,
          url: `${window.location.origin}/console/invitations/${state.token}`,
        }
      : null;
  const outcome =
    invitation && attempt?.token === invitation.token ? attempt.outcome : null;

  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-semibold">Invite a member</h2>
      <form action={formAction} className="flex flex-wrap items-end gap-3">
        <input type="hidden" name="org" value={org} />
        <div className="flex flex-col gap-2">
          <Label htmlFor="role">Role</Label>
          <KeptSelect
            id="role"
            name="role"
            value={role}
            onChange={(event) => setRole(event.target.value)}
            className="w-40"
          >
            <option value="member">Member</option>
            <option value="admin">Admin</option>
          </KeptSelect>
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
      {invitation && (
        <div className="flex flex-col gap-2">
          <p className="text-sm text-muted-foreground">
            Share this link with the person you invited. It works once and then
            expires.
          </p>
          <div className="flex max-w-lg gap-2">
            <Input
              readOnly
              value={invitation.url}
              aria-label="Invitation link"
              onFocus={(event) => event.target.select()}
              className="min-w-0 flex-1 bg-muted font-mono text-xs"
            />
            <Button
              type="button"
              variant="outline"
              onClick={async () => {
                // No boundary catches a handler. `navigator.clipboard`
                // is absent outside a secure context, so the try covers
                // the property access and not only the rejection.
                try {
                  await navigator.clipboard.writeText(invitation.url);
                  setAttempt({ token: invitation.token, outcome: "copied" });
                } catch {
                  setAttempt({ token: invitation.token, outcome: "failed" });
                }
              }}
            >
              {outcome === "copied" ? "Copied" : "Copy"}
            </Button>
          </div>
          {outcome === "failed" && (
            <p role="alert" className="text-sm text-destructive">
              Could not copy the link. Select it in the field and copy it
              yourself.
            </p>
          )}
        </div>
      )}
    </section>
  );
}
