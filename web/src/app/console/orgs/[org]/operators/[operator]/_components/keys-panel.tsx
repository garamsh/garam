"use client";

import { KeyRound, RefreshCw } from "lucide-react";
import Link from "next/link";
import { useState } from "react";

import { DataTable, type ColumnDef } from "@/components/data-table";
import { StatusDot } from "@/components/status-dot";
import { Time } from "@/components/time";
import { Button } from "@/components/ui/button";
import {
  revokeOperatorKey,
  type RevokeOperatorKeyState,
} from "@/features/orgs/actions";
import type { OperatorKey } from "@/features/orgs";
import { useHydrated } from "@/hooks/use-hydrated";

import { ActionDialog } from "../../../../../_components/action-dialog";
import { EmptyState } from "../../../../../_components/empty-state";
import { CopyableCode } from "../../_components/copyable-code";
import {
  KEY_STATUS,
  MAX_LIVE_KEYS,
  liveKeys,
} from "../../_components/operator-status";

const initialState: RevokeOperatorKeyState = { status: "idle" };

/**
 * A key's fingerprint, shortened for the table and copyable whole. It is
 * what an assertion names the key by, so it is what a person compares
 * against the key installed in the cluster.
 */
function Fingerprint({ kid }: { kid: string }) {
  return (
    <CopyableCode
      value={kid}
      shown={`${kid.slice(0, 12)}…`}
      label="fingerprint"
    />
  );
}

/** Withdraws one key, after saying what stops working. */
function RevokeKey({
  org,
  operator,
  kid,
}: {
  org: string;
  operator: string;
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
            It stops verifying, and every token exchanged under it stops
            working, the moment you confirm. An operator still signing with it
            is refused. Its record stays, listed as revoked.
          </p>
        }
        action={revokeOperatorKey}
        initialState={initialState}
        fields={{ org, operator, kid }}
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
 * Every key the operator was issued, withdrawn ones included, with where
 * each stands and when it last obtained a token (ADR-0102). Issuing the
 * first key and rotating are the operator's setup page; revoking is
 * here. `canIssue` and `canRevoke` are false only where the server
 * denied the action.
 */
export function KeysPanel({
  org,
  operator,
  keys,
  setupHref,
  canIssue,
  canRevoke,
}: {
  org: string;
  operator: string;
  keys: OperatorKey[];
  setupHref: string;
  canIssue: boolean;
  canRevoke: boolean;
}) {
  const live = liveKeys(keys);
  const columns: ColumnDef<OperatorKey, unknown>[] = [
    {
      accessorKey: "kid",
      header: "Fingerprint",
      enableSorting: false,
      cell: ({ row }) => <Fingerprint kid={row.original.kid} />,
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
      accessorKey: "issuedAt",
      header: "Issued",
      cell: ({ row }) => <Time value={row.original.issuedAt} />,
    },
    {
      accessorKey: "lastExchangedAt",
      header: "Last sign-in",
      cell: ({ row }) =>
        row.original.lastExchangedAt ? (
          <Time value={row.original.lastExchangedAt} />
        ) : (
          <span className="text-muted-foreground">Never</span>
        ),
    },
    {
      id: "actions",
      header: () => <span className="sr-only">Actions</span>,
      enableSorting: false,
      cell: ({ row }) =>
        canRevoke && row.original.state !== "revoked" ? (
          <RevokeKey org={org} operator={operator} kid={row.original.kid} />
        ) : null,
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <p className="max-w-xl text-muted-foreground">
          The operator signs in to garam with its key. garam keeps only the
          public half, so a lost key is replaced, never shown again.
        </p>
        {canIssue && (
          <Button asChild variant={live.length === 0 ? "default" : "outline"}>
            <Link href={live.length === 0 ? setupHref : `${setupHref}?rotate`}>
              {live.length === 0 ? <KeyRound /> : <RefreshCw />}
              {live.length === 0
                ? "Issue a key"
                : live.length >= MAX_LIVE_KEYS
                  ? "Replace keys"
                  : "Rotate key"}
            </Link>
          </Button>
        )}
      </div>
      <DataTable
        label="Keys"
        columns={columns}
        data={keys}
        getRowId={(key) => key.kid}
        empty={
          <EmptyState
            icon={KeyRound}
            title="No keys issued"
          />
        }
      />
    </div>
  );
}
