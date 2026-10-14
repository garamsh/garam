"use client";

import { useActionState, useState } from "react";

import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import {
  withdrawOrgConsent,
  type WithdrawOrgConsentState,
} from "@/features/orgs/actions";

const initialState: WithdrawOrgConsentState = { status: "idle" };

/**
 * Withdraws this organization's half of one pair. What it undoes is
 * live traffic, so it asks first, in a dialog whose button names the
 * act and whose text says what stops (ADR-0103 §Patterns).
 *
 * `stands` decides which consequence is true. Withdrawing a half the
 * other side has matched stops messages that cross today and leaves
 * that side's half up for a later re-grant to join; withdrawing one it
 * has not stops nothing and takes the relation away (ADR-0035).
 *
 * The dialog stays open until the action answers, so a refusal is read
 * in it; a withdrawal closes it and the card re-renders.
 */
export function WithdrawConsentDialog({
  org,
  other,
  stands,
}: {
  org: string;
  other: string;
  stands: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [state, formAction, pending] = useActionState(
    async (previous: WithdrawOrgConsentState, formData: FormData) => {
      const next = await withdrawOrgConsent(previous, formData);
      if (next.status !== "error") setOpen(false);
      return next;
    },
    initialState,
  );

  return (
    <AlertDialog open={open} onOpenChange={setOpen}>
      <div>
        <AlertDialogTrigger asChild>
          <Button variant="outline">Withdraw consent</Button>
        </AlertDialogTrigger>
      </div>
      <AlertDialogContent>
        <form action={formAction} className="grid gap-4">
          <AlertDialogHeader>
            <AlertDialogTitle>Withdraw consent for {other}?</AlertDialogTitle>
            <AlertDialogDescription>
              {stands
                ? `Messages between this organization and ${other} stop at once. ${other} keeps its own half, so granting again here would let them cross.`
                : `Nothing crosses today, because ${other} has not granted its half, and withdrawing removes the relation.`}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <input type="hidden" name="org" value={org} />
          <input type="hidden" name="other" value={other} />
          {state.status === "error" && (
            <p role="alert" className="text-danger">
              {state.message}
            </p>
          )}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={pending}>Cancel</AlertDialogCancel>
            <Button type="submit" variant="destructive" disabled={pending}>
              {pending ? "Withdrawing…" : "Withdraw consent"}
            </Button>
          </AlertDialogFooter>
        </form>
      </AlertDialogContent>
    </AlertDialog>
  );
}
