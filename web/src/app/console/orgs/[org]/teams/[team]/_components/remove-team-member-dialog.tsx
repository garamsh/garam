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

type Props = {
  org: string;
  team: string;
  teamName: string;
  member: string;
  label: string;
};

/**
 * Removes one member from this team, after a dialog naming both. The
 * member keeps its place in the organization and in other teams, and
 * the dialog says so, since that is the question a person about to
 * press it is asking. A refusal stays in the dialog that asked, and a
 * removal closes it.
 */
export function RemoveTeamMemberDialog(props: Props) {
  const [open, setOpen] = useState(false);

  return (
    <AlertDialog open={open} onOpenChange={setOpen}>
      <AlertDialogTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          aria-label={`Remove ${props.label} from ${props.teamName}`}
        >
          Remove
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <RemoveForm {...props} onRemoved={() => setOpen(false)} />
      </AlertDialogContent>
    </AlertDialog>
  );
}

/**
 * The dialog's form. It is mounted only while the dialog is open, so a
 * refusal it shows goes when the dialog closes.
 */
function RemoveForm({
  org,
  team,
  teamName,
  member,
  label,
  onRemoved,
}: Props & { onRemoved: () => void }) {
  const [state, formAction, pending] = useActionState(
    async (previous: RemoveGroupMemberState, formData: FormData) => {
      const answer = await removeGroupMember(previous, formData);
      if (answer.status === "idle") onRemoved();
      return answer;
    },
    initialState,
  );

  return (
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
  );
}
