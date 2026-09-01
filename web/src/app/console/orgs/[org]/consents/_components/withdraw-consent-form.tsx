"use client";

import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  withdrawOrgConsent,
  type WithdrawOrgConsentState,
} from "@/features/orgs/actions";

const initialState: WithdrawOrgConsentState = { status: "idle" };

/**
 * Withdraws this organization's half of one pair. What it undoes is
 * live traffic, so the button asks first and the confirmation says what
 * stops: a single click withdrawing on the spot would put a channel
 * agents are using behind a misclick.
 *
 * `stands` decides which consequence is true. Withdrawing a half the
 * other side has matched stops messages that cross today and leaves
 * that side's half up for a later re-grant to join; withdrawing one it
 * has not stops nothing and takes the relation away (ADR-0035).
 */
export function WithdrawConsentForm({
  org,
  other,
  stands,
}: {
  org: string;
  other: string;
  stands: boolean;
}) {
  const [state, formAction, pending] = useActionState(
    withdrawOrgConsent,
    initialState,
  );
  const [confirming, setConfirming] = useState(false);

  return (
    <div className="flex flex-col gap-2">
      {confirming ? (
        <form action={formAction} className="flex flex-col gap-3">
          <input type="hidden" name="org" value={org} />
          <input type="hidden" name="other" value={other} />
          <p className="text-sm">
            {stands
              ? `Messages between this organization and ${other} stop at once. ${other} keeps its own half, so granting again here would let them cross.`
              : `Nothing crosses today, because ${other} has not granted its half, and withdrawing removes the relation.`}
          </p>
          <div className="flex flex-wrap gap-2">
            <Button type="submit" variant="destructive" disabled={pending}>
              {pending ? "Withdrawing…" : "Confirm withdrawal"}
            </Button>
            <Button
              type="button"
              variant="ghost"
              disabled={pending}
              onClick={() => setConfirming(false)}
            >
              Cancel
            </Button>
          </div>
        </form>
      ) : (
        <div>
          <Button
            type="button"
            variant="outline"
            onClick={() => setConfirming(true)}
          >
            Withdraw
          </Button>
        </div>
      )}
      {state.status === "error" && (
        <p role="alert" className="text-sm text-destructive">
          {state.message}
        </p>
      )}
    </div>
  );
}
