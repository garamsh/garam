"use client";

import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  removeGroupMember,
  type RemoveGroupMemberState,
} from "@/features/orgs/actions";

const initialState: RemoveGroupMemberState = { status: "idle" };

/**
 * Removes one member from this team, after a confirmation naming both.
 * The member keeps its place in the organization and in other teams,
 * and the confirmation says so, since that is the question a person
 * about to press it is asking.
 */
export function RemoveTeamMemberForm({
  org,
  team,
  teamName,
  member,
  label,
}: {
  org: string;
  team: string;
  teamName: string;
  member: string;
  label: string;
}) {
  const [state, formAction, pending] = useActionState(
    removeGroupMember,
    initialState,
  );
  const [confirming, setConfirming] = useState(false);

  return (
    <div className="flex flex-col items-end gap-2">
      {confirming ? (
        <form action={formAction} className="flex max-w-sm flex-col gap-3">
          <input type="hidden" name="org" value={org} />
          <input type="hidden" name="group" value={team} />
          <input type="hidden" name="member" value={member} />
          <p className="text-sm">
            Remove {label} from {teamName}? Only this team membership goes;
            nothing else changes in the organization or in other teams.
          </p>
          <div className="flex flex-wrap gap-2">
            <Button type="submit" variant="destructive" disabled={pending}>
              {pending ? "Removing…" : "Confirm removal"}
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
        <Button
          type="button"
          variant="outline"
          size="sm"
          aria-label={`Remove ${label} from ${teamName}`}
          onClick={() => setConfirming(true)}
        >
          Remove
        </Button>
      )}
      {state.status === "error" && (
        <p role="alert" className="text-sm text-destructive">
          {state.message}
        </p>
      )}
    </div>
  );
}
