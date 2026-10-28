"use client";

import { Button } from "@garam/design/ui/button";
import type { RemoveGroupMemberState } from "@/features/orgs/actions";

import { ActionDialog } from "../action-dialog";
import { SCOPE_KIND, type MembershipScope } from "./scope";

const initialState: RemoveGroupMemberState = { status: "idle" };

/**
 * Removes one member from a team or a project, after a dialog naming
 * both. The member keeps its place in the organization and in the
 * others of that kind, and the dialog says so, since that is the
 * question a person about to press it is asking.
 */
export function RemoveMemberDialog({
  org,
  scope,
  member,
  label,
}: {
  org: string;
  scope: MembershipScope;
  member: string;
  label: string;
}) {
  const kind = SCOPE_KIND[scope.kind];
  return (
    <ActionDialog
      trigger={
        <Button
          type="button"
          variant="ghost"
          size="sm"
          aria-label={`Remove ${label} from ${scope.name}`}
        >
          Remove
        </Button>
      }
      title={`Remove ${label} from ${scope.name}?`}
      description={
        <p>
          Only this {kind.noun} membership goes. Nothing else changes in the
          organization or in other {kind.plural}.
        </p>
      }
      action={kind.remove}
      initialState={initialState}
      fields={{ org, [kind.field]: scope.id, member }}
      confirmLabel={`Remove from ${kind.noun}`}
      pendingLabel="Removing…"
      destructive
      done={(state) => state.status === "idle"}
    />
  );
}
