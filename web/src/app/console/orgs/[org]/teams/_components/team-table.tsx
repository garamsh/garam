"use client";

import Link from "next/link";
import { UsersRound } from "lucide-react";

import { CopyId } from "@/components/copy-id";
import { DataTable, type ColumnDef } from "@/components/data-table";
import { Badge } from "@/components/ui/badge";

import { EmptyState } from "../../../../_components/empty-state";
import { orgHref } from "../../../../_components/navigation";
import { ROLE_LABEL, type OrgRole } from "../../../../_components/org-role";

/** One team as its row shows it. */
export type TeamRow = {
  name: string;
  /** The segment the team's URL carries (ADR-0011). */
  identifier: string;
  roles: readonly OrgRole[];
};

function columns(org: string): ColumnDef<TeamRow, unknown>[] {
  return [
    {
      id: "team",
      header: "Team",
      accessorFn: (team) => `${team.name} ${team.identifier}`,
      sortingFn: (a, b) => a.original.name.localeCompare(b.original.name),
      cell: ({ row: { original: team } }) => (
        // The gap keeps the name link and the CopyId each a 24px
        // target apart (WCAG 2.2 SC 2.5.8).
        <div className="flex min-w-0 flex-col gap-2">
          <Link
            href={`${orgHref(org, "teams")}/${team.identifier}`}
            className="font-medium underline-offset-4 hover:underline focus-visible:underline"
          >
            {team.name}
          </Link>
          <CopyId value={team.identifier} label={team.name} className="flex" />
        </div>
      ),
    },
    {
      id: "roles",
      header: "Roles it grants",
      accessorFn: (team) =>
        team.roles.map((role) => ROLE_LABEL[role]).join(" "),
      enableSorting: false,
      cell: ({ row: { original: team } }) =>
        team.roles.length === 0 ? (
          <span className="text-muted-foreground">None</span>
        ) : (
          <div className="flex flex-wrap gap-1.5">
            {team.roles.map((role) => (
              <Badge key={role} variant="secondary">
                {ROLE_LABEL[role]}
              </Badge>
            ))}
          </div>
        ),
    },
  ];
}

/**
 * The organization's teams by name, each linking to its own page, with
 * the identifier beneath to copy and the roles each grants its members.
 */
export function TeamTable({
  org,
  teams,
}: {
  org: string;
  teams: readonly TeamRow[];
}) {
  return (
    <DataTable
      label="Teams"
      columns={columns(org)}
      data={[...teams]}
      getRowId={(team) => team.identifier}
      search={{ placeholder: "Search teams" }}
      empty={
        <EmptyState
          icon={UsersRound}
          title="No teams"
        />
      }
    />
  );
}
