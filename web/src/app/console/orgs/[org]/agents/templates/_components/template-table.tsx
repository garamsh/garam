"use client";

import Link from "next/link";

import { CopyId } from "@/components/copy-id";
import { DataTable, type ColumnDef } from "@/components/data-table";
import { Time } from "@/components/time";

/** One archived template as the table lists it. */
export type TemplateRow = {
  identifier: string;
  name: string;
  description: string;
  valueCount: number;
  updatedAt: string;
};

function columns(org: string): ColumnDef<TemplateRow, unknown>[] {
  return [
    {
      id: "name",
      header: "Template",
      // Names repeat, so the identifier is searched and shown too.
      accessorFn: (row) => `${row.name} ${row.identifier}`,
      cell: ({ row: { original: row } }) => (
        <div className="flex min-w-0 flex-col gap-0.5">
          <Link
            href={`/console/orgs/${org}/agents/templates/${row.identifier}`}
            className="font-medium wrap-anywhere underline-offset-4 hover:underline focus-visible:underline"
          >
            {row.name}
          </Link>
          <CopyId value={row.identifier} label={row.name} />
        </div>
      ),
    },
    {
      id: "description",
      header: "Description",
      accessorKey: "description",
      enableSorting: false,
      cell: ({ row: { original: row } }) =>
        row.description === "" ? (
          <span className="text-muted-foreground">None</span>
        ) : (
          row.description
        ),
    },
    {
      id: "values",
      header: "Values",
      accessorKey: "valueCount",
      enableGlobalFilter: false,
      cell: ({ row: { original: row } }) => (
        <span className="tabular-nums">{row.valueCount}</span>
      ),
    },
    {
      id: "updated",
      header: "Last changed",
      accessorKey: "updatedAt",
      enableGlobalFilter: false,
      cell: ({ row: { original: row } }) => <Time value={row.updatedAt} />,
    },
  ];
}

/**
 * The organization's archived templates, each linking to the page that
 * shows its record. A template's values are shown there rather than
 * here.
 */
export function TemplateTable({
  org,
  rows,
  empty,
}: {
  org: string;
  rows: TemplateRow[];
  empty: React.ReactNode;
}) {
  return (
    <DataTable
      label="Archived templates"
      columns={columns(org)}
      data={rows}
      getRowId={(row) => row.identifier}
      empty={empty}
      search={{ placeholder: "Search archived templates" }}
    />
  );
}
