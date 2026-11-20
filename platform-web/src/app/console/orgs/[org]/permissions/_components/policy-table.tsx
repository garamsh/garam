"use client";

import Link from "next/link";
import { ShieldCheck } from "lucide-react";

import { CopyId } from "@/components/copy-id";
import { DataTable, type ColumnDef } from "@/components/data-table";
import type { IamPolicy } from "@/features/orgs";

import { EmptyState } from "../../../../_components/empty-state";
import { POLICY_KIND_LABEL } from "./policy-kind";
import { POLICY_STATUS_LABEL, PolicyStatus } from "./policy-status";

/** A policy as its row reads it, with the address its page is at. */
export type PolicyRow = { policy: IamPolicy; href: string };

const COLUMNS: ColumnDef<PolicyRow, unknown>[] = [
  {
    id: "name",
    header: "Policy",
    accessorFn: (row) => row.policy.name,
    // The name link is about 20px tall; the gap keeps its 24px target
    // clear of the CopyId under it.
    cell: ({ row }) => (
      <div className="flex min-w-0 flex-col items-start gap-2">
        <Link
          href={row.original.href}
          className="font-medium underline-offset-4 hover:underline focus-visible:underline"
        >
          {row.original.policy.name}
        </Link>
        <CopyId
          value={row.original.policy.id}
          label={row.original.policy.name}
        />
      </div>
    ),
  },
  {
    id: "kind",
    header: "Kind",
    accessorFn: (row) => POLICY_KIND_LABEL[row.policy.kind],
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
];

/**
 * The organization's policies by name, each opening its own page, with
 * what each is, where it stands and the version it decides by while
 * active.
 */
export function PolicyTable({ rows }: { rows: PolicyRow[] }) {
  return (
    <DataTable
      label="Policies"
      columns={COLUMNS}
      data={rows}
      getRowId={(row) => row.policy.id}
      search={{ placeholder: "Search policies" }}
      empty={<EmptyState icon={ShieldCheck} title="No policies" />}
    />
  );
}
