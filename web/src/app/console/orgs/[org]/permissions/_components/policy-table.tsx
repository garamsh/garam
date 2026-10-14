"use client";

import Link from "next/link";
import { ShieldCheck } from "lucide-react";

import { CopyId } from "@/components/copy-id";
import { DataTable, type ColumnDef } from "@/components/data-table";
import { Time } from "@/components/time";
import type { Policy } from "@/features/orgs";

import { EmptyState } from "../../../../_components/empty-state";
import { POLICY_STATUS_LABEL, PolicyStatus } from "./policy-status";

/** A policy as its row reads it, with the address its page is at. */
export type PolicyRow = { policy: Policy; identifier: string; href: string };

const COLUMNS: ColumnDef<PolicyRow, unknown>[] = [
  {
    id: "name",
    header: "Policy",
    accessorFn: (row) => row.policy.name,
    cell: ({ row }) => (
      <div className="flex min-w-0 flex-col items-start gap-2">
        <Link
          href={row.original.href}
          className="font-medium underline-offset-4 hover:underline focus-visible:underline"
        >
          {row.original.policy.name}
        </Link>
        <CopyId
          value={row.original.identifier}
          label={row.original.policy.name}
        />
      </div>
    ),
  },
  {
    id: "status",
    header: "Status",
    accessorFn: (row) => POLICY_STATUS_LABEL[row.policy.status],
    cell: ({ row }) => <PolicyStatus status={row.original.policy.status} />,
  },
  {
    id: "version",
    header: "Default version",
    accessorFn: (row) => row.policy.defaultVersion,
    cell: ({ row }) => (
      <span className="font-mono tabular-nums">
        v{row.original.policy.defaultVersion}
      </span>
    ),
  },
  {
    id: "updated",
    header: "Updated",
    accessorFn: (row) => row.policy.updatedAt,
    enableGlobalFilter: false,
    cell: ({ row }) => <Time value={row.original.policy.updatedAt} />,
  },
];

/**
 * The organization's policies by name, each opening its own page, with
 * where each stands and the version it evaluates while active.
 */
export function PolicyTable({ rows }: { rows: PolicyRow[] }) {
  return (
    <DataTable
      label="Policies"
      columns={COLUMNS}
      data={rows}
      getRowId={(row) => row.identifier}
      search={{ placeholder: "Search policies" }}
      empty={
        <EmptyState
          icon={ShieldCheck}
          title="No policies"
          description="This organization has no policies."
        />
      }
    />
  );
}
