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

/** The outcome as the service names it, a write's in the past tense. */
function outcomeLabel(record: AuditRecord): string {
  return record.kind === "write" ? record.outcome : `${record.outcome}`;
}

/** What a record adds to its outcome: a write's step, a denial's count. */
function detail(record: AuditRecord): string | null {
  if (record.kind === "write") {
    return record.versionFrom === 0
      ? `v${record.versionTo}`
      : `v${record.versionFrom} to v${record.versionTo}`;
  }
  return record.count > 1 ? `${record.count} times` : "Once";
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
      row.original.subject === "" ? (
        <span className="text-muted-foreground">The platform</span>
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
        <span className="font-mono text-xs">{row.original.action}</span>
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
    accessorFn: (record) => outcomeLabel(record),
    cell: ({ row }) => (
      <div className="flex flex-col items-start gap-1">
        <span>{outcomeLabel(row.original)}</span>
        <span className="text-xs text-muted-foreground tabular-nums">
          {detail(row.original)}
        </span>
      </div>
    ),
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
