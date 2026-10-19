"use client";

import { Link2Off } from "lucide-react";

import { CopyId } from "@/components/copy-id";
import { DataTable, type ColumnDef } from "@/components/data-table";

import { EmptyState } from "../../../../../_components/empty-state";
import { DetachPolicyDialog } from "../../_components/detach-policy-dialog";

/**
 * One attachment as its row shows it, worded on the server, where the
 * organization's lists were read.
 */
export type AttachmentRow = {
  grn: string;
  /** What the organization calls it; null where it has no name. */
  label: string | null;
  identifier: string;
  kind: string;
  /** How a sentence names it: its name, or what it is. */
  sentenceLabel: string;
  /** What the lookup could not settle, said under the name. */
  note: { tone: "danger" | "muted"; text: string } | null;
};

/**
 * The subjects a policy is attached to, by name and type, each
 * detachable where the server does not deny it. One the organization no
 * longer holds stays listed and marked; one whose list the caller may
 * not read is marked unchecked.
 */
export function AttachmentTable({
  org,
  policy,
  policyName,
  subjects,
  canDetach,
  empty,
}: {
  org: string;
  policy: string;
  policyName: string;
  subjects: AttachmentRow[];
  canDetach: boolean;
  /** What a policy with no attachment reaches, said in its place. */
  empty: string;
}) {
  const columns: ColumnDef<AttachmentRow, unknown>[] = [
    {
      id: "name",
      header: "Attached to",
      accessorFn: (subject) => subject.label ?? subject.identifier,
      cell: ({ row: { original: subject } }) => (
        <div className="flex min-w-0 flex-col items-start gap-0.5 whitespace-normal">
          {subject.label === null ? (
            <span className="text-muted-foreground">No name available</span>
          ) : (
            <span className="font-medium">{subject.label}</span>
          )}
          {subject.label !== subject.identifier && (
            <CopyId
              value={subject.identifier}
              label={subject.label ?? "this subject"}
            />
          )}
          {subject.note !== null && (
            <span
              className={
                subject.note.tone === "danger"
                  ? "text-xs text-destructive"
                  : "text-xs text-muted-foreground"
              }
            >
              {subject.note.text}
            </span>
          )}
        </div>
      ),
    },
    {
      id: "type",
      header: "Type",
      accessorFn: (subject) => subject.kind,
    },
    {
      id: "actions",
      header: () => <span className="sr-only">Actions</span>,
      enableSorting: false,
      cell: ({ row: { original: subject } }) =>
        canDetach && (
          <div className="flex justify-end">
            <DetachPolicyDialog
              org={org}
              policy={policy}
              subject={subject.grn}
              policyName={policyName}
              subjectLabel={subject.sentenceLabel}
            />
          </div>
        ),
    },
  ];
  return (
    <DataTable
      label="Attachments"
      columns={columns}
      data={subjects}
      getRowId={(subject) => subject.grn}
      empty={
        <EmptyState
          icon={Link2Off}
          title={empty}
        />
      }
    />
  );
}
