"use client";

import { FolderGit2 } from "lucide-react";
import Link from "next/link";

import { CopyId } from "@/components/copy-id";
import { DataTable, type ColumnDef } from "@/components/data-table";
import { Badge } from "@/components/ui/badge";
import type { Project } from "@/features/orgs";

import { EmptyState } from "../../../../_components/empty-state";

/** One row: the project and the identifier its paths take. */
export type ProjectRow = {
  name: string;
  identifier: string;
  roles: Project["roles"];
};

const columns = (org: string): ColumnDef<ProjectRow, unknown>[] => [
  {
    accessorKey: "name",
    header: "Name",
    cell: ({ row }) => (
      <Link
        href={`/console/orgs/${org}/projects/${row.original.identifier}`}
        className="font-medium wrap-anywhere underline-offset-4 hover:underline focus-visible:underline"
      >
        {row.original.name}
      </Link>
    ),
  },
  {
    accessorKey: "identifier",
    header: "Identifier",
    enableSorting: false,
    cell: ({ row }) => (
      <CopyId value={row.original.identifier} label={row.original.name} />
    ),
  },
  {
    id: "roles",
    header: "Roles it grants",
    enableSorting: false,
    enableGlobalFilter: false,
    cell: ({ row }) =>
      row.original.roles.length === 0 ? (
        <span className="text-muted-foreground">None</span>
      ) : (
        <span className="flex flex-wrap gap-1.5">
          {row.original.roles.map((role) => (
            <Badge key={role} variant="secondary">
              {role}
            </Badge>
          ))}
        </span>
      ),
  },
];

/**
 * The organization's projects by name, each with the identifier an
 * agent's address names it by and the roles it grants its own members.
 */
export function ProjectTable({
  org,
  rows,
}: {
  org: string;
  rows: ProjectRow[];
}) {
  return (
    <DataTable
      label="Projects"
      columns={columns(org)}
      data={rows}
      getRowId={(row) => row.identifier}
      search={{ placeholder: "Search projects" }}
      empty={
        <EmptyState
          icon={FolderGit2}
          title="No projects"
          description="This organization has no projects."
        />
      }
    />
  );
}
