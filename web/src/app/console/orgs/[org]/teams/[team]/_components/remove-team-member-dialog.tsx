"use client";

import { Button } from "@/components/ui/button";
import {
  removeGroupMember,
  type RemoveGroupMemberState,
} from "@/features/orgs/actions";

import { ActionDialog } from "../../../../../_components/action-dialog";

const initialState: RemoveGroupMemberState = { status: "idle" };

/**
 * Removes one member from this team, after a dialog naming both. The
 * member keeps its place in the organization and in other teams, and
 * the dialog says so, since that is the question a person about to
 * press it is asking.
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
  return (
    <ActionDialog
      trigger={
        <Button
          type="button"
          variant="ghost"
          size="sm"
          aria-label={`Remove ${label} from ${teamName}`}
        >
          Remove
        </Button>
      }
      title={`Remove ${label} from ${teamName}?`}
      description={
        <p>
          Only this team membership goes. Nothing else changes in the
          organization or in other teams.
        </p>
      }
      action={removeGroupMember}
      initialState={initialState}
      fields={{ org, group: team, member }}
      confirmLabel="Remove from team"
      pendingLabel="Removing…"
      destructive
      done={(state) => state.status === "idle"}
    />
  );
}
