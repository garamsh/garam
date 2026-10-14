"use client";

import { UserRound } from "lucide-react";

import { CopyId } from "@/components/copy-id";
import { DataTable, type ColumnDef } from "@/components/data-table";
import { Time } from "@/components/time";
import type { Member } from "@/features/orgs";

import { EmptyState } from "../../../../_components/empty-state";
import { ROLE_LABEL } from "../../../../_components/org-role";

/** One member as its row shows it. */
export type MemberRow = {
  /** The segment the API addresses the member by (ADR-0011). */
  identifier: string;
  /** Null where the member's user row is gone. */
  nickname: string | null;
  role: Member["role"];
  since: string;
};

const NO_NAME = "No name on record";

const COLUMNS: ColumnDef<MemberRow, unknown>[] = [
  {
    id: "name",
    header: "Name",
    accessorFn: (member) =>
      `${member.nickname ?? NO_NAME} ${member.identifier}`,
    sortingFn: (a, b) =>
      (a.original.nickname ?? "").localeCompare(b.original.nickname ?? ""),
    cell: ({ row: { original: member } }) => (
      <div className="flex min-w-0 flex-col gap-0.5">
        {member.nickname === null ? (
          <span className="text-muted-foreground">{NO_NAME}</span>
        ) : (
          <span className="font-medium">{member.nickname}</span>
        )}
        <CopyId
          value={member.identifier}
          label={member.nickname ?? "this member"}
          className="flex"
        />
      </div>
    ),
  },
  {
    id: "role",
    header: "Role",
    accessorFn: (member) => ROLE_LABEL[member.role],
    cell: ({ row: { original: member } }) => ROLE_LABEL[member.role],
  },
  {
    id: "since",
    header: "Member since",
    accessorFn: (member) => member.since,
    enableGlobalFilter: false,
    cell: ({ row: { original: member } }) => <Time value={member.since} />,
  },
];

/**
 * The organization's members by name, role and joining time. A member
 * is shown by the name the user row carries, and the identifier the API
 * addresses them by is offered beneath it to copy rather than as text
 * to read.
 *
 * `nickname` is absent only where that row is gone
 * (`api/openapi.yaml` §Member), and the cell then says so rather than
 * lifting the identifier into the name's place: an identity subject
 * put where a name goes reads as a name and names nobody.
 */
export function MemberTable({ members }: { members: readonly MemberRow[] }) {
  return (
    <DataTable
      label="People"
      columns={COLUMNS}
      data={[...members]}
      getRowId={(member) => member.identifier}
      search={{ placeholder: "Search people" }}
      empty={
        <EmptyState
          icon={UserRound}
          title="No people"
          description="No one belongs to this organization."
        />
      }
    />
  );
}
