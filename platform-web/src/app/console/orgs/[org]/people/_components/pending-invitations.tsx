"use client";

import { DataTable, type ColumnDef } from "@/components/data-table";
import { Time } from "@/components/time";
import { Button } from "@garam/design/ui/button";
import {
  revokeInvitation,
  type RevokeInvitationState,
} from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";

import { ActionDialog } from "../../../../_components/action-dialog";
import { ROLE_LABEL, type OrgRole } from "../../../../_components/org-role";
import { Section } from "../../../../_components/section";

const initialState: RevokeInvitationState = { status: "idle" };

/** One pending invitation, its inviter already resolved to a name. */
export type PendingInvitationRow = {
  id: string;
  role: OrgRole;
  invitedBy: string;
  createdAt: string;
  expiresAt: string;
};

/** Revokes one invitation, after saying what stops working. */
function RevokeInvitation({ org, id }: { org: string; id: string }) {
  const hydrated = useHydrated();
  return (
    <div className="flex justify-end">
      <ActionDialog
        trigger={
          <Button variant="ghost" size="sm" disabled={!hydrated}>
            Revoke
          </Button>
        }
        title="Revoke this invitation?"
        description={<p>Its link stops working for anyone who holds it.</p>}
        action={revokeInvitation}
        initialState={initialState}
        fields={{ org, invitation: id }}
        confirmLabel="Revoke invitation"
        pendingLabel="Revoking…"
        destructive
        done={(state) => state.status === "revoked"}
      />
    </div>
  );
}

/**
 * The invitations that can still be accepted (ADR-0136), each named by
 * who sent it and when, since a link is shown once and never again.
 * With none pending the part is not there at all: the list above is the
 * page's subject.
 */
export function PendingInvitations({
  org,
  invitations,
}: {
  org: string;
  invitations: PendingInvitationRow[];
}) {
  if (invitations.length === 0) return null;
  const columns: ColumnDef<PendingInvitationRow, unknown>[] = [
    {
      accessorKey: "role",
      header: "Role",
      cell: ({ row }) => ROLE_LABEL[row.original.role],
    },
    { accessorKey: "invitedBy", header: "Invited by" },
    {
      accessorKey: "createdAt",
      header: "Sent",
      cell: ({ row }) => <Time value={row.original.createdAt} />,
    },
    {
      accessorKey: "expiresAt",
      header: "Expires",
      cell: ({ row }) => <Time value={row.original.expiresAt} />,
    },
    {
      id: "actions",
      header: () => <span className="sr-only">Actions</span>,
      enableSorting: false,
      cell: ({ row }) => <RevokeInvitation org={org} id={row.original.id} />,
    },
  ];

  return (
    <Section title="Pending invitations" count={invitations.length} region>
      <DataTable
        label="Pending invitations"
        columns={columns}
        data={invitations}
        getRowId={(invitation) => invitation.id}
        empty={null}
      />
    </Section>
  );
}
