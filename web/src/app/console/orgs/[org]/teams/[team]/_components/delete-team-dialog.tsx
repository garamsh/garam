"use client";

import { Button } from "@/components/ui/button";
import { deleteGroup, type DeleteGroupState } from "@/features/orgs/actions";

import { ActionDialog } from "../../../../../_components/action-dialog";

const initialState: DeleteGroupState = { status: "idle" };

/**
 * Deletes the team after a dialog naming it and what goes with it.
 * garam brings a deleted team back by no route, so one click is not
 * enough. A deleted team opens the list.
 */
export function DeleteTeamDialog({
  org,
  team,
  teamName,
  memberships,
}: {
  org: string;
  team: string;
  teamName: string;
  /** How many memberships go with it, as a phrase. */
  memberships: string;
}) {
  return (
    <ActionDialog
      trigger={
        <Button type="button" variant="destructive">
          Delete team
        </Button>
      }
      title={`Delete ${teamName}?`}
      description={
        <p>
          This removes the team and its {memberships}. The people and agents in
          it stay in this organization. garam cannot bring the team back.
        </p>
      }
      action={deleteGroup}
      initialState={initialState}
      fields={{ org, group: team }}
      command={{
        label: "Delete team",
        consequence:
          "Removes the team and its memberships, for good; its members stay.",
      }}
      confirmLabel="Delete team"
      pendingLabel="Deleting…"
      destructive
      done={(state) => state.status !== "error"}
    />
  );
}
