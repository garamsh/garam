"use client";

import { Blocks } from "lucide-react";
import Link from "next/link";

import { DataTable, type ColumnDef } from "@/components/data-table";
import { StatusDot } from "@/components/status-dot";

import { EmptyState } from "../../../../_components/empty-state";
import { INSTANCES_LABEL } from "./app-labels";
import { InstallDialog, type InstallableApp } from "./install-dialog";

/** One app a row shows: the app and whether the organization installed it. */
export type AppRow = InstallableApp & { installed: boolean };

/**
 * The apps the organization may install, one row each: what it is, how
 * many instances it runs, and either the page of its installation or
 * the way to install it. `projects` are the choices of an install's
 * scope; `canInstall` is false only where the server denied installing.
 */
export function AppTable({
  org,
  appsHref,
  apps,
  projects,
  canInstall,
}: {
  org: string;
  appsHref: string;
  apps: AppRow[];
  projects: { grn: string; name: string }[];
  canInstall: boolean;
}) {
  const columns: ColumnDef<AppRow, unknown>[] = [
    {
      id: "app",
      accessorFn: (row) => `${row.name} ${row.slug} ${row.description}`,
      header: "App",
      cell: ({ row }) => (
        <div className="flex min-w-0 flex-col py-1.5">
          <span className="font-medium wrap-anywhere">{row.original.name}</span>
          <span className="font-mono text-xs text-muted-foreground">
            {row.original.slug}
          </span>
          {row.original.description && (
            <span className="text-muted-foreground">
              {row.original.description}
            </span>
          )}
        </div>
      ),
    },
    {
      id: "instances",
      accessorFn: (row) => INSTANCES_LABEL[row.instances],
      header: "Instances",
      cell: ({ row }) => INSTANCES_LABEL[row.original.instances],
    },
    {
      id: "installed",
      accessorFn: (row) => (row.installed ? "Installed" : "Not installed"),
      header: "Installation",
      cell: ({ row }) =>
        row.original.installed ? (
          <span className="flex flex-wrap items-center gap-x-3">
            <StatusDot tone="ok" label="Installed" />
            <Link
              href={`${appsHref}/${row.original.slug}`}
              className="underline-offset-4 hover:underline"
              aria-label={`Open ${row.original.name}`}
            >
              Open
            </Link>
          </span>
        ) : canInstall ? (
          <InstallDialog org={org} app={row.original} projects={projects} />
        ) : (
          <StatusDot tone="neutral" label="Not installed" />
        ),
    },
  ];

  return (
    <DataTable
      label="Apps"
      columns={columns}
      data={apps}
      getRowId={(row) => row.slug}
      search={{ placeholder: "Search apps" }}
      empty={<EmptyState icon={Blocks} title="No app can be installed" />}
    />
  );
}
