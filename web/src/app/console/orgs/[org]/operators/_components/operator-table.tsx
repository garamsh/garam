"use client";

import { Server as ServerIcon } from "lucide-react";
import Link from "next/link";

import { DataTable, type ColumnDef } from "@/components/data-table";
import { StatusDot } from "@/components/status-dot";

import { EmptyState } from "../../../../_components/empty-state";
import {
  LastContact,
  ReportedImages,
  ReportsNotice,
  reportOf,
  useOperatorReports,
  type ReportsSource,
} from "./operator-reports";
import { certificateStatus } from "./operator-status";

/** One operator as the list shows it. */
export type OperatorRow = {
  grn: string;
  identifier: string;
  name: string;
  certificateIssuedAt?: string;
  controlEndpoint?: string;
};

/** The control service's host, which is what tells two endpoints apart. */
function host(endpoint: string): string {
  try {
    return new URL(endpoint).host;
  } catch {
    return endpoint;
  }
}

/**
 * The organization's operators by name, each opening its own page. A
 * name is what the organization calls an operator and two may share
 * one, so the identifier stands under every name. Where `reports` is
 * given, each row also says what images the operator reported and
 * when it last made contact; a page passes null where the server
 * denies the caller that read.
 */
export function OperatorTable({
  operatorsHref,
  operators,
  reports,
}: {
  operatorsHref: string;
  operators: OperatorRow[];
  reports: ReportsSource | null;
}) {
  const read = useOperatorReports(reports);
  const columns: ColumnDef<OperatorRow, unknown>[] = [
    {
      id: "operator",
      accessorFn: (row) => `${row.name} ${row.identifier}`,
      header: "Operator",
      cell: ({ row }) => (
        <div className="flex min-w-0 flex-col py-1.5">
          <Link
            href={`${operatorsHref}/${row.original.identifier}`}
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
      id: "control",
      accessorFn: (row) => row.controlEndpoint ?? "",
      header: "Control service",
      cell: ({ row }) =>
        row.original.controlEndpoint ? (
          <span className="font-mono text-xs">
            {host(row.original.controlEndpoint)}
          </span>
        ) : (
          <span className="text-muted-foreground">Not registered</span>
        ),
    },
    {
      id: "certificate",
      accessorFn: (row) => certificateStatus(row.certificateIssuedAt).label,
      header: "Certificate",
      cell: ({ row }) => (
        <StatusDot {...certificateStatus(row.original.certificateIssuedAt)} />
      ),
    },
    ...(read === null
      ? []
      : ([
          {
            id: "images",
            header: "Images",
            enableSorting: false,
            enableGlobalFilter: false,
            cell: ({ row }) => (
              <ReportedImages
                state={reportOf(read, row.original.grn)}
                withDigest={false}
              />
            ),
          },
          {
            id: "lastContact",
            header: "Last contact",
            enableSorting: false,
            enableGlobalFilter: false,
            cell: ({ row }) => (
              <LastContact state={reportOf(read, row.original.grn)} />
            ),
          },
        ] satisfies ColumnDef<OperatorRow, unknown>[])),
  ];

  return (
    <div className="flex flex-col gap-4">
      {read !== null && (
        <ReportsNotice read={read} operatorsHref={operatorsHref} />
      )}
      <DataTable
        label="Operators"
        columns={columns}
        data={operators}
        getRowId={(row) => row.identifier}
        search={{ placeholder: "Search operators" }}
        empty={<EmptyState icon={ServerIcon} title="No operators registered" />}
      />
    </div>
  );
}
