"use client";

import { useActionState } from "react";

import { Button } from "@/components/ui/button";
import {
  acceptInvitation,
  type AcceptInvitationState,
} from "@/features/orgs/api/accept-invitation";

const initialState: AcceptInvitationState = { status: "idle" };

export function AcceptInvitationForm({ token }: { token: string }) {
  const [state, formAction, pending] = useActionState(
    acceptInvitation,
    initialState,
  );

  return (
    <form action={formAction} className="flex flex-col items-start gap-4">
      <input type="hidden" name="token" value={token} />
      {state.status === "error" && (
        <p role="alert" className="text-sm text-destructive">
          {state.message}
        </p>
      )}
      <Button type="submit" disabled={pending}>
        {pending ? "Accepting…" : "Accept invitation"}
      </Button>
    </form>
  );
}
