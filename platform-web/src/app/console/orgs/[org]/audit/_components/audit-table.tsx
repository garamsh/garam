"use client";

import { ScrollText } from "lucide-react";

import { CopyId } from "@/components/copy-id";
import { DataTable, type ColumnDef } from "@/components/data-table";
import { Time } from "@/components/time";
import type { AuditRecord } from "@/features/orgs";

import { EmptyState } from "../../../../_components/empty-state";

const KIND_LABEL: Record<AuditRecord["kind"], string> = {
  write: "Policy write",
  denial: "Denial",
  "denial-overflow": "Denials over the day's limit",
};

/**
 * What a record adds to its outcome. A write's step is the version it
 * wrote or moved to, from the default it moved from where there was
 * one; a removal names no version, so it adds none. A denial's count
 * is how many identical denials it holds, and an overflow record's the
 * denials of its UTC day that were not stored.
 */
function detail(record: AuditRecord): string | null {
  switch (record.kind) {
    case "write":
      if (record.versionTo === 0) return null;
      return record.versionFrom === 0
        ? `v${record.versionTo}`
        : `v${record.versionFrom} to v${record.versionTo}`;
    case "denial":
      return record.count > 1 ? `${record.count} times` : "Once";
    case "denial-overflow":
      return `${record.count} not stored${
        record.day ? ` on ${record.day.slice(0, 10)}` : ""
      }`;
  }
}

const COLUMNS: ColumnDef<AuditRecord, unknown>[] = [
  {
    id: "time",
    header: "When",
    enableSorting: false,
    cell: ({ row }) => <Time value={row.original.lastAt} />,
  },
  {
    id: "who",
    header: "Who asked",
    enableSorting: false,
    accessorFn: (record) => record.subject,
    cell: ({ row }) =>
      row.original.kind === "denial-overflow" ? (
        <span className="text-muted-foreground">Not recorded</span>
      ) : row.original.subject === "" ? (
        <span className="text-muted-foreground">No one named</span>
      ) : (
        <CopyId value={row.original.subject} label="Subject" />
      ),
  },
  {
    id: "action",
    header: "Action",
    enableSorting: false,
    accessorFn: (record) => `${KIND_LABEL[record.kind]} ${record.action}`,
    cell: ({ row }) => (
      <div className="flex min-w-0 flex-col items-start gap-1">
        {row.original.action !== "" && (
          <span className="font-mono text-xs">{row.original.action}</span>
        )}
        <span className="text-xs text-muted-foreground">
          {KIND_LABEL[row.original.kind]}
        </span>
      </div>
    ),
  },
  {
    id: "resource",
    header: "Resource",
    enableSorting: false,
    accessorFn: (record) => record.resource,
    cell: ({ row }) =>
      row.original.resource === "" ? (
        <span className="text-muted-foreground">None</span>
      ) : (
        <CopyId value={row.original.resource} label="Resource" />
      ),
  },
  {
    id: "outcome",
    header: "Outcome",
    enableSorting: false,
    accessorFn: (record) => record.outcome,
    cell: ({ row }) => {
      const added = detail(row.original);
      return (
        <div className="flex flex-col items-start gap-1">
          {row.original.outcome !== "" && <span>{row.original.outcome}</span>}
          {added && (
            <span className="text-xs text-muted-foreground tabular-nums">
              {added}
            </span>
          )}
        </div>
      );
    },
  },
  {
    id: "policies",
    header: "Policies",
    enableSorting: false,
    accessorFn: (record) => record.policyIds.join(" "),
    cell: ({ row }) =>
      row.original.policyIds.length === 0 ? (
        <span className="text-muted-foreground">None</span>
      ) : (
        <div className="flex flex-col items-start gap-1">
          {row.original.policyIds.map((id) => (
            <CopyId key={id} value={id} label="Policy" />
          ))}
        </div>
      ),
  },
];

/**
 * One page of the organization's audit records, as the service ordered
 * them: newest first. A column does not sort, because a sort would
 * reorder one page and read as the order of all of them.
 */
export function AuditTable({ records }: { records: AuditRecord[] }) {
  return (
    <DataTable
      label="Audit records"
      columns={COLUMNS}
      data={records}
      getRowId={(record) => String(record.id)}
      search={{ placeholder: "Search this page" }}
      empty={<EmptyState icon={ScrollText} title="No audit records" />}
    />
  );
}
