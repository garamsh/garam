"use client";

import { useActionState } from "react";
import Link from "next/link";

import { Refusal } from "@/components/refusal";
import { Button } from "@/components/ui/button";
import {
  acceptInvitation,
  type AcceptInvitationState,
} from "@/features/orgs/actions";

const initialState: AcceptInvitationState = { status: "idle" };

/** Carries the token out of the URL and into the accept action. */
export function AcceptInvitationForm({
  token,
  orgName,
}: {
  token: string;
  orgName: string;
}) {
  const [state, formAction, pending] = useActionState(
    acceptInvitation,
    initialState,
  );

  return (
    <form action={formAction} className="flex flex-col items-start gap-3">
      <input type="hidden" name="token" value={token} />
      {state.status === "error" && <Refusal>{state.message}</Refusal>}
      <div className="flex flex-wrap items-center gap-3">
        <Button type="submit" disabled={pending}>
          {pending ? "Joining…" : `Join ${orgName}`}
        </Button>
        <Button asChild variant="ghost">
          <Link href="/console/orgs">Not now</Link>
        </Button>
      </div>
    </form>
  );
}
