"use client";

import { Laptop, Terminal } from "lucide-react";

import { DataTable, type ColumnDef } from "@/components/data-table";
import { Time } from "@/components/time";
import { Badge } from "@garam/design/ui/badge";
import { Button } from "@garam/design/ui/button";
import {
  revokeSession,
  type RevokeSessionState,
} from "@/features/auth/actions";
import type { Session } from "@/features/auth";

import { ActionDialog } from "../../_components/action-dialog";
import { EmptyState } from "../../_components/empty-state";

const initialState: RevokeSessionState = { status: "idle" };

/** What a session is called in its row and its dialog. */
function sessionName(session: Session): string {
  if (session.kind === "browser") return "Browser";
  return session.name ? `CLI on ${session.name}` : "CLI";
}

function RevokeButton({ session }: { session: Session }) {
  const name = sessionName(session);
  return (
    <ActionDialog<RevokeSessionState>
      trigger={
        <Button
          type="button"
          variant="ghost"
          size="sm"
          aria-label={`Revoke ${name}${session.current ? " (this session)" : ""}`}
        >
          Revoke
        </Button>
      }
      title={`Revoke ${name}?`}
      description={
        <p>
          {session.current
            ? "This signs you out here."
            : "It stops working from its next request."}
        </p>
      }
      action={revokeSession}
      initialState={initialState}
      fields={{ session: session.id }}
      confirmLabel="Revoke session"
      pendingLabel="Revoking…"
      destructive
      done={(state) => state.status === "revoked"}
    />
  );
}

const COLUMNS: ColumnDef<Session, unknown>[] = [
  {
    id: "session",
    header: "Session",
    accessorFn: (row) => sessionName(row),
    cell: ({ row: { original: row } }) => (
      <span className="flex items-center gap-2">
        {row.kind === "cli" ? (
          <Terminal aria-hidden className="size-4 text-muted-foreground" />
        ) : (
          <Laptop aria-hidden className="size-4 text-muted-foreground" />
        )}
        {sessionName(row)}
        {row.current && <Badge variant="outline">This session</Badge>}
      </span>
    ),
  },
  {
    id: "created",
    header: "Signed in",
    accessorFn: (row) => row.createdAt,
    cell: ({ row: { original: row } }) => <Time value={row.createdAt} />,
  },
  {
    id: "expires",
    header: "Expires",
    accessorFn: (row) => row.expiresAt,
    cell: ({ row: { original: row } }) => <Time value={row.expiresAt} />,
  },
  {
    id: "actions",
    header: () => <span className="sr-only">Actions</span>,
    enableSorting: false,
    cell: ({ row: { original: row } }) => <RevokeButton session={row} />,
  },
];

/** The person's sessions, the current one marked, each revocable. */
export function SessionTable({ sessions }: { sessions: Session[] }) {
  return (
    <DataTable
      label="Sessions"
      columns={COLUMNS}
      data={sessions}
      getRowId={(row) => row.id}
      empty={<EmptyState icon={Laptop} title="No sessions" />}
    />
  );
}
