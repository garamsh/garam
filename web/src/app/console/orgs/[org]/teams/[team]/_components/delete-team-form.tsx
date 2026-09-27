"use client";

import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import { deleteGroup, type DeleteGroupState } from "@/features/orgs/actions";

const initialState: DeleteGroupState = { status: "idle" };

/**
 * Deletes the team after a confirmation naming it and what goes with
 * it. garam brings a deleted team back by no route, so one click is not
 * enough.
 */
export function DeleteTeamForm({
  org,
  team,
  teamName,
  memberCount,
}: {
  org: string;
  team: string;
  teamName: string;
  memberCount: number;
}) {
  const [state, formAction, pending] = useActionState(
    deleteGroup,
    initialState,
  );
  const [confirming, setConfirming] = useState(false);
  const memberships =
    memberCount === 1 ? "1 membership" : `${memberCount} memberships`;

  return (
    <div className="flex flex-col gap-2">
      {confirming ? (
        <form action={formAction} className="flex max-w-2xl flex-col gap-3">
          <input type="hidden" name="org" value={org} />
          <input type="hidden" name="group" value={team} />
          <p className="text-sm">
            Delete {teamName}? This removes the team and its {memberships}; the
            people and agents in it stay in this organization. garam cannot
            bring the team back.
          </p>
          <div className="flex flex-wrap gap-2">
            <Button type="submit" variant="destructive" disabled={pending}>
              {pending ? "Deleting…" : `Delete ${teamName}`}
            </Button>
            <Button
              type="button"
              variant="ghost"
              disabled={pending}
              autoFocus
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
            Delete team
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
