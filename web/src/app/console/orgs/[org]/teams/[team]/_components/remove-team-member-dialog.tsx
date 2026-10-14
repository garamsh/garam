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
  removeGroupMember,
  type RemoveGroupMemberState,
} from "@/features/orgs/actions";

const initialState: RemoveGroupMemberState = { status: "idle" };

/**
 * Removes one member from this team, after a dialog naming both. The
 * member keeps its place in the organization and in other teams, and
 * the dialog says so, since that is the question a person about to
 * press it is asking. A refusal stays in the dialog that asked, and a
 * removal closes it.
 */
export function RemoveTeamMemberDialog({
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
  // The answer the dialog opened on: a later answer that is not a
  // refusal is the removal done.
  const [openedOn, setOpenedOn] = useState<RemoveGroupMemberState | null>(null);
  const open =
    openedOn !== null && (state === openedOn || state.status === "error");

  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => setOpenedOn(next ? state : null)}
    >
      <AlertDialogTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          aria-label={`Remove ${label} from ${teamName}`}
        >
          Remove
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <form action={formAction} className="grid gap-4">
          <input type="hidden" name="org" value={org} />
          <input type="hidden" name="group" value={team} />
          <input type="hidden" name="member" value={member} />
          <AlertDialogHeader>
            <AlertDialogTitle>
              Remove {label} from {teamName}?
            </AlertDialogTitle>
            <AlertDialogDescription>
              Only this team membership goes. Nothing else changes in the
              organization or in other teams.
            </AlertDialogDescription>
          </AlertDialogHeader>
          {state.status === "error" && (
            <p role="alert" className="text-sm text-danger">
              {state.message}
            </p>
          )}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={pending}>Cancel</AlertDialogCancel>
            <Button type="submit" variant="destructive" disabled={pending}>
              {pending ? "Removing…" : "Remove from team"}
            </Button>
          </AlertDialogFooter>
        </form>
      </AlertDialogContent>
    </AlertDialog>
  );
}
