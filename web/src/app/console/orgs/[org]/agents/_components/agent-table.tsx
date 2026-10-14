"use client";

import Link from "next/link";

import { CopyId } from "@/components/copy-id";
import { DataTable, type ColumnDef } from "@/components/data-table";
import { StatusDot } from "@/components/status-dot";
import { Time } from "@/components/time";

import type { AgentDot } from "./agent-status";
import { OperatorName } from "./agent-facts";

/** One agent as the list shows it, read and decided on the server. */
export type AgentRow = {
  identifier: string;
  /** Its definition's name, or what stands in where it has none. */
  name: string;
  /** Says why a reported agent carries no name, where it does not. */
  nameNote: string | null;
  operator: { identifier: string; name: string | undefined } | null;
  requested: AgentDot;
  provisioning: AgentDot;
  runtime: AgentDot;
  /** When a report last landed, absent before any report. */
  updatedAt: string | null;
};

function columns(org: string): ColumnDef<AgentRow, unknown>[] {
  return [
    {
      id: "agent",
      header: "Agent",
      // The identifier is searched too: names repeat and it does not.
      accessorFn: (row) => `${row.name} ${row.identifier}`,
      cell: ({ row: { original: row } }) => (
        <div className="flex min-w-0 flex-col gap-0.5">
          <Link
            href={`/console/orgs/${org}/agents/${row.identifier}`}
            className="font-medium wrap-anywhere underline-offset-4 hover:underline focus-visible:underline"
          >
            {row.name}
          </Link>
          <CopyId value={row.identifier} label={row.name} />
          {row.nameNote !== null && (
            <span className="text-xs text-muted-foreground">
              {row.nameNote}
            </span>
          )}
        </div>
      ),
    },
    {
      id: "operator",
      header: "Operator",
      accessorFn: (row) => row.operator?.name ?? row.operator?.identifier ?? "",
      cell: ({ row: { original: row } }) =>
        row.operator === null ? (
          <span className="text-muted-foreground">None named</span>
        ) : (
          <OperatorName org={org} {...row.operator} />
        ),
    },
    {
      id: "requested",
      header: "Requested",
      accessorFn: (row) => row.requested.label,
      cell: ({ row: { original: row } }) => <StatusDot {...row.requested} />,
    },
    {
      id: "provisioning",
      header: "Provisioning",
      accessorFn: (row) => row.provisioning.label,
      cell: ({ row: { original: row } }) => <StatusDot {...row.provisioning} />,
    },
    {
      id: "runtime",
      header: "Runtime",
      accessorFn: (row) => row.runtime.label,
      cell: ({ row: { original: row } }) => <StatusDot {...row.runtime} />,
    },
    {
      id: "updated",
      header: "Reported",
      accessorFn: (row) => row.updatedAt ?? "",
      enableGlobalFilter: false,
      cell: ({ row: { original: row } }) =>
        row.updatedAt === null ? (
          <span className="text-muted-foreground">Never</span>
        ) : (
          <Time value={row.updatedAt} />
        ),
    },
  ];
}

/**
 * The organization's agents as a table: what was requested beside the
 * two states reported, each a dot and a label. The rows arrive in the
 * order the page put them, failing ones first.
 */
export function AgentTable({
  org,
  rows,
  empty,
}: {
  org: string;
  rows: AgentRow[];
  empty: React.ReactNode;
}) {
  return (
    <DataTable
      label="Agents"
      columns={columns(org)}
      data={rows}
      getRowId={(row) => row.identifier}
      empty={empty}
      search={{ placeholder: "Search agents" }}
    />
  );
}
