"use client";

import { Boxes } from "lucide-react";
import Link from "next/link";

import { DataTable, type ColumnDef } from "@/components/data-table";
import { Time } from "@/components/time";

import { EmptyState } from "../../../../../_components/empty-state";

/** One instance a row shows. `lastContact` is null where it has made none, and undefined where it was not read. */
export type InstanceRow = {
  identifier: string;
  name: string;
  kind: string;
  lastContact: string | null | undefined;
};

/**
 * The installation's instances, one row each: its name opening its
 * page, the kind as the organization typed it, and when it last made
 * contact. A kind is whatever string was stored and is shown as text,
 * never matched against anything (ADR-0192 decision 1).
 */
export function InstanceTable({
  instancesHref,
  instances,
}: {
  instancesHref: string;
  instances: InstanceRow[];
}) {
  const columns: ColumnDef<InstanceRow, unknown>[] = [
    {
      id: "instance",
      accessorFn: (row) => `${row.name} ${row.identifier}`,
      header: "Instance",
      cell: ({ row }) => (
        <div className="flex min-w-0 flex-col py-1.5">
          <Link
            href={`${instancesHref}/${row.original.identifier}`}
            className="font-medium wrap-anywhere underline-offset-4 hover:underline"
          >
            {row.original.name}
          </Link>
          <span className="font-mono text-xs text-muted-foreground">
            {row.original.identifier}
          </span>
        </div>
      ),
    },
    {
      accessorKey: "kind",
      header: "Kind",
      cell: ({ row }) =>
        row.original.kind || (
          <span className="text-muted-foreground">Not said</span>
        ),
    },
    {
      id: "lastContact",
      header: "Last contact",
      enableSorting: false,
      enableGlobalFilter: false,
      cell: ({ row }) => <LastContactCell value={row.original.lastContact} />,
    },
  ];

  return (
    <DataTable
      label="Instances"
      columns={columns}
      data={instances}
      getRowId={(row) => row.identifier}
      search={{ placeholder: "Search instances" }}
      empty={<EmptyState icon={Boxes} title="No instance created" />}
    />
  );
}

/** When an instance last made contact, or why that is not said. */
export function LastContactCell({
  value,
}: {
  value: string | null | undefined;
}) {
  if (value === undefined) {
    return <span className="text-muted-foreground">Not read</span>;
  }
  if (value === null) {
    return <span className="text-muted-foreground">Never</span>;
  }
  return <Time value={value} />;
}
