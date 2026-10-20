"use client";

import { Server as ServerIcon } from "lucide-react";
import Link from "next/link";

import { DataTable, type ColumnDef } from "@/components/data-table";

import { EmptyState } from "../../../../_components/empty-state";
import {
  LastContact,
  ReportedImages,
  ReportsNotice,
  reportOf,
  useOperatorReports,
  type ReportsSource,
} from "./operator-reports";
import type { OperatorIdentity, OperatorRow } from "./operator-pairs";

/** The control service's host, which is what tells two endpoints apart. */
function host(endpoint: string): string {
  try {
    return new URL(endpoint).host;
  } catch {
    return endpoint;
  }
}

/** An identity by name, opening its own page, its identifier under it. */
function IdentityCell({
  identity,
  operatorsHref,
}: {
  identity: OperatorIdentity;
  operatorsHref: string;
}) {
  return (
    <div className="flex min-w-0 flex-col py-1.5">
      <Link
        href={`${operatorsHref}/${identity.identifier}`}
        className="font-medium wrap-anywhere underline-offset-4 hover:underline"
      >
        {identity.name}
      </Link>
      <span className="font-mono text-xs text-muted-foreground">
        {identity.identifier}
      </span>
      {identity.controlEndpoint && (
        <span className="font-mono text-xs text-muted-foreground">
          {host(identity.controlEndpoint)}
        </span>
      )}
    </div>
  );
}

/**
 * The organization's Operators, one row each: the manager and the
 * control service that names it (ADR-0124), each by name and opening
 * its own page. A name is what the organization calls an identity and
 * two may share one, so the identifier stands under every name, with
 * the host its control endpoint answers on where it has one. Where `reports` is
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
      id: "manager",
      accessorFn: (row) => `${row.manager?.name ?? ""} ${row.identifier}`,
      header: "Cluster manager",
      cell: ({ row }) =>
        row.original.manager ? (
          <IdentityCell
            identity={row.original.manager}
            operatorsHref={operatorsHref}
          />
        ) : (
          <div className="flex min-w-0 flex-col py-1.5">
            <span className="text-muted-foreground">Not registered</span>
            <span className="font-mono text-xs text-muted-foreground">
              {row.original.identifier}
            </span>
          </div>
        ),
    },
    {
      id: "control",
      accessorFn: (row) =>
        row.controlService
          ? `${row.controlService.name} ${row.controlService.identifier}`
          : "",
      header: "Control service",
      cell: ({ row }) =>
        row.original.controlService ? (
          <IdentityCell
            identity={row.original.controlService}
            operatorsHref={operatorsHref}
          />
        ) : (
          <span className="text-muted-foreground">Not connected</span>
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
        getRowId={(row) => row.grn}
        search={{ placeholder: "Search operators" }}
        empty={<EmptyState icon={ServerIcon} title="No operators registered" />}
      />
    </div>
  );
}
