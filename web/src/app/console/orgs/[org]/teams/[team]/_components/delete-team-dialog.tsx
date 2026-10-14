"use client";

import { useActionState } from "react";

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
import { deleteGroup, type DeleteGroupState } from "@/features/orgs/actions";

const initialState: DeleteGroupState = { status: "idle" };

type Props = {
  org: string;
  team: string;
  teamName: string;
  /** How many memberships go with it, as a phrase. */
  memberships: string;
};

/**
 * Deletes the team after a dialog naming it and what goes with it.
 * garam brings a deleted team back by no route, so one click is not
 * enough. A deleted team opens the list; a refusal stays in the dialog
 * and goes when it closes.
 */
export function DeleteTeamDialog(props: Props) {
  return (
    <AlertDialog>
      <AlertDialogTrigger asChild>
        <Button type="button" variant="destructive">
          Delete team
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <DeleteForm {...props} />
      </AlertDialogContent>
    </AlertDialog>
  );
}

/** The dialog's form, mounted only while the dialog is open. */
function DeleteForm({ org, team, teamName, memberships }: Props) {
  const [state, formAction, pending] = useActionState(
    deleteGroup,
    initialState,
  );

  return (
    <form action={formAction} className="grid gap-4">
      <input type="hidden" name="org" value={org} />
      <input type="hidden" name="group" value={team} />
      <AlertDialogHeader>
        <AlertDialogTitle>Delete {teamName}?</AlertDialogTitle>
        <AlertDialogDescription>
          This removes the team and its {memberships}. The people and agents in
          it stay in this organization. garam cannot bring the team back.
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
          {pending ? "Deleting…" : "Delete team"}
        </Button>
      </AlertDialogFooter>
    </form>
  );
}
