"use client";

import { KeyRound } from "lucide-react";
import { useState } from "react";

import { DataTable, type ColumnDef } from "@/components/data-table";
import { StatusDot } from "@/components/status-dot";
import { Time } from "@/components/time";
import { Button } from "@garam/design/ui/button";
import type { InstanceKey } from "@/features/orgs";
import {
  revokeInstanceKey,
  type RevokeInstanceKeyState,
} from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";

import { ActionDialog } from "../../../../../../_components/action-dialog";
import { EmptyState } from "../../../../../../_components/empty-state";
import { CopyableCode } from "../../../../operators/_components/copyable-code";
import { KEY_STATUS } from "../../../_components/app-labels";

const initialState: RevokeInstanceKeyState = { status: "idle" };

/** Withdraws one key, after saying what stops working. */
function RevokeKey({
  org,
  installation,
  instance,
  kid,
}: {
  org: string;
  installation: string;
  instance: string;
  kid: string;
}) {
  const hydrated = useHydrated();
  const [revoked, setRevoked] = useState(false);
  if (revoked) {
    return (
      <span role="status" className="text-muted-foreground">
        Revoked
      </span>
    );
  }
  return (
    <div className="flex justify-end">
      <ActionDialog
        trigger={
          <Button variant="ghost" size="sm" disabled={!hydrated}>
            Revoke
          </Button>
        }
        title="Revoke this key?"
        description={
          <p>
            The instance can no longer sign in with it, and every token
            exchanged under it stops working. Reissuing the registration token
            is how it gets a new one.
          </p>
        }
        action={revokeInstanceKey}
        initialState={initialState}
        fields={{ org, installation, instance, kid }}
        confirmLabel="Revoke key"
        pendingLabel="Revoking…"
        destructive
        done={(state) => state.status === "revoked"}
        onAnswer={(state) => setRevoked(state.status === "revoked")}
      />
    </div>
  );
}

/**
 * Every key the instance registered, withdrawn ones included, with
 * where each stands (ADR-0184 decision 7). A key is `retiring` until
 * the moment garam stops accepting it, which the row says. Revoking is
 * here; `canRevoke` is false only where the server denied it.
 */
export function KeysPanel({
  org,
  installation,
  instance,
  keys,
  canRevoke,
}: {
  org: string;
  installation: string;
  instance: string;
  keys: InstanceKey[];
  canRevoke: boolean;
}) {
  const columns: ColumnDef<InstanceKey, unknown>[] = [
    {
      accessorKey: "kid",
      header: "Fingerprint",
      enableSorting: false,
      cell: ({ row }) => (
        <CopyableCode
          value={row.original.kid}
          shown={`${row.original.kid.slice(0, 12)}…`}
          label="fingerprint"
        />
      ),
    },
    {
      accessorKey: "state",
      header: "State",
      cell: ({ row }) => {
        const key = row.original;
        return (
          <span className="flex flex-wrap items-center gap-x-1.5">
            <StatusDot {...KEY_STATUS[key.state]} />
            {key.state === "retiring" && key.retiresAt && (
              <span className="text-muted-foreground">
                until <Time value={key.retiresAt} />
              </span>
            )}
            {key.state === "revoked" && key.revokedAt && (
              <span className="text-muted-foreground">
                <Time value={key.revokedAt} />
              </span>
            )}
          </span>
        );
      },
    },
    {
      accessorKey: "createdAt",
      header: "Registered",
      cell: ({ row }) => <Time value={row.original.createdAt} />,
    },
    {
      id: "actions",
      header: () => <span className="sr-only">Actions</span>,
      enableSorting: false,
      cell: ({ row }) =>
        canRevoke && row.original.state !== "revoked" ? (
          <RevokeKey
            org={org}
            installation={installation}
            instance={instance}
            kid={row.original.kid}
          />
        ) : null,
    },
  ];

  return (
    <DataTable
      label="Keys"
      columns={columns}
      data={keys}
      getRowId={(key) => key.kid}
      empty={
        <EmptyState
          icon={KeyRound}
          title="No key registered yet. The connector registers one with the registration token."
        />
      }
    />
  );
}
