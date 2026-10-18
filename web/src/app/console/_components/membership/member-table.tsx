"use client";

import { UsersRound } from "lucide-react";

import { CopyId } from "@/components/copy-id";
import { DataTable, type ColumnDef } from "@/components/data-table";
import { StatusDot } from "@/components/status-dot";
import type { Lookup } from "@/features/orgs";

import { EmptyState } from "../empty-state";
import { RemoveMemberDialog } from "./remove-member-dialog";
import { SCOPE_KIND, type MembershipScope } from "./scope";

/** One membership as its row shows it, every phrase already chosen. */
export type MemberRow = {
  grn: string;
  identifier: string;
  /** Person, Agent or Unknown. */
  type: string;
  /** The member's name, or what stands in its place. */
  name: string;
  /** False where `name` stands in for one the page does not have. */
  named: boolean;
  /** How a sentence names the member. */
  sentence: string;
  lookup: Lookup;
  /** The list the member's name would come from, as a plural noun. */
  listed: "people" | "agents";
};

function columns(
  org: string,
  scope: MembershipScope,
  removable: boolean,
): ColumnDef<MemberRow, unknown>[] {
  const shown: ColumnDef<MemberRow, unknown>[] = [
    {
      id: "member",
      header: "Member",
      accessorFn: (member) => `${member.name} ${member.identifier}`,
      sortingFn: (a, b) => a.original.name.localeCompare(b.original.name),
      cell: ({ row: { original: member } }) => (
        <div className="flex min-w-0 flex-col gap-0.5 whitespace-normal">
          <span
            className={member.named ? "font-medium" : "text-muted-foreground"}
          >
            {member.name}
          </span>
          <CopyId
            value={member.identifier}
            label={member.name}
            className="flex"
          />
        </div>
      ),
    },
    {
      id: "type",
      header: "Type",
      accessorFn: (member) => member.type,
      cell: ({ row: { original: member } }) => member.type,
    },
    {
      id: "status",
      header: "Status",
      accessorFn: (member) => member.lookup,
      enableSorting: false,
      cell: ({ row: { original: member } }) =>
        member.lookup === "missing" ? (
          <StatusDot tone="danger" label="Not found in this organization" />
        ) : member.lookup === "refused" ? (
          <span className="flex flex-col gap-0.5 whitespace-normal">
            <StatusDot tone="neutral" label="Not checked" />
            <span className="text-xs text-muted-foreground">
              This organization&apos;s {member.listed} are not listed to you
            </span>
          </span>
        ) : (
          <StatusDot tone="ok" label="In this organization" />
        ),
    },
  ];
  if (removable) {
    shown.push({
      id: "actions",
      header: () => <span className="sr-only">Actions</span>,
      enableSorting: false,
      enableGlobalFilter: false,
      cell: ({ row: { original: member } }) => (
        <div className="flex justify-end">
          <RemoveMemberDialog
            org={org}
            scope={scope}
            member={member.grn}
            label={member.sentence}
          />
        </div>
      ),
    });
  }
  return shown;
}

/**
 * A team's or a project's members by name, type and whether the
 * organization still holds them, each removable from it where the
 * server allows it.
 *
 * A member without a name says so rather than lifting its identifier
 * into the name's place, and one the organization no longer holds is
 * marked rather than dropped: either would otherwise read as someone
 * the organization knows. One whose list the caller may not read is
 * marked unchecked, never missing.
 */
export function MemberTable({
  org,
  scope,
  removable,
  members,
}: {
  org: string;
  scope: MembershipScope;
  removable: boolean;
  members: readonly MemberRow[];
}) {
  return (
    <DataTable
      label={`Members of ${scope.name}`}
      columns={columns(org, scope, removable)}
      data={[...members]}
      getRowId={(member) => member.grn}
      search={{ placeholder: "Search members" }}
      empty={
        <EmptyState
          icon={UsersRound}
          title="No members"
          description={`This ${SCOPE_KIND[scope.kind].noun} has no members.`}
        />
      }
    />
  );
}
