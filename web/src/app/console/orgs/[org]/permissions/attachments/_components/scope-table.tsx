"use client";

import Link from "next/link";
import { ShieldCheck } from "lucide-react";

import { DataTable, type ColumnDef } from "@/components/data-table";
import type { PolicyStatus as Status } from "@/features/orgs";

import { EmptyState } from "../../../../../_components/empty-state";
import {
  POLICY_STATUS_LABEL,
  PolicyStatus,
} from "../../_components/policy-status";

/** One subject a policy is attached to, as a link where it can open. */
export type ScopeSubject = { key: string; text: string; href: string | null };

/** A policy and whom it reaches, worded on the server. */
export type ScopeRow = {
  identifier: string;
  name: string;
  href: string;
  status: Status;
  reach:
    | { kind: "attached"; subjects: ScopeSubject[] }
    | { kind: "everyone" }
    | { kind: "none" }
    | { kind: "unknown" };
};

/** The reach as plain text, which the table searches and sorts by. */
function reachText(row: ScopeRow): string {
  switch (row.reach.kind) {
    case "attached":
      return row.reach.subjects.map((subject) => subject.text).join(", ");
    case "everyone":
      return "Everyone in this organization";
    case "none":
      return "No attachments";
    case "unknown":
      return "Not checked";
  }
}

const COLUMNS: ColumnDef<ScopeRow, unknown>[] = [
  {
    id: "policy",
    header: "Policy",
    accessorFn: (row) => row.name,
    cell: ({ row }) => (
      <Link
        href={row.original.href}
        className="font-medium underline-offset-4 hover:underline focus-visible:underline"
      >
        {row.original.name}
      </Link>
    ),
  },
  {
    id: "status",
    header: "Status",
    accessorFn: (row) => POLICY_STATUS_LABEL[row.status],
    cell: ({ row }) => <PolicyStatus status={row.original.status} />,
  },
  {
    id: "reach",
    header: "Attached to",
    accessorFn: reachText,
    enableSorting: false,
    cell: ({ row: { original: row } }) => {
      switch (row.reach.kind) {
        case "unknown":
          return (
            <span className="text-muted-foreground">
              Not checked: not listed to you
            </span>
          );
        case "everyone":
          return (
            <span className="font-medium">Everyone in this organization</span>
          );
        case "none":
          return <span className="text-muted-foreground">No attachments</span>;
        case "attached":
          return (
            <ul className="flex flex-wrap gap-x-1 whitespace-normal">
              {row.reach.subjects.map((subject, index, all) => (
                <li key={subject.key}>
                  {subject.href === null ? (
                    subject.text
                  ) : (
                    <Link
                      href={subject.href}
                      className="underline-offset-4 hover:underline focus-visible:underline"
                    >
                      {subject.text}
                    </Link>
                  )}
                  {index < all.length - 1 && ","}
                </li>
              ))}
            </ul>
          );
      }
    },
  },
];

/**
 * Every policy of the organization with whom it is attached to. An
 * active policy with no attachment reaches everyone; one whose
 * attachments were refused says its scope is unknown rather than
 * claiming either.
 */
export function ScopeTable({ rows }: { rows: ScopeRow[] }) {
  return (
    <DataTable
      label="Policies and their attachments"
      columns={COLUMNS}
      data={rows}
      getRowId={(row) => row.identifier}
      search={{ placeholder: "Search policies or subjects" }}
      empty={
        <EmptyState
          icon={ShieldCheck}
          title="No policies"
        />
      }
    />
  );
}
