"use client";

import { History } from "lucide-react";

import { DataTable, type ColumnDef } from "@/components/data-table";
import { JsonDiff } from "@/components/json-diff";
import { StatusDot } from "@/components/status-dot";
import type { IamPolicyBody } from "@/features/orgs";
import { Button } from "@garam/design/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@garam/design/ui/dialog";

import { EmptyState } from "../../../../../_components/empty-state";
import { MakeDefaultDialog } from "./make-default-dialog";
import { PolicyBody, bodyText } from "./policy-body";

/** One stored version as the table shows it. */
export type VersionRow = {
  version: number;
  body: IamPolicyBody;
  /** Who wrote it, as the policy service names them. */
  author: string;
};

/** What every row's make-default control needs to know. */
export type VersionTableContext = {
  org: string;
  policy: string;
  defaultVersion: number;
  /** The default version's body, or undefined if it was not stored. */
  defaultBody: IamPolicyBody | undefined;
  active: boolean;
  /** Whether making a version the default is offered. */
  canWrite: boolean;
};

/** One version's body, against the default where it is not it. */
function CompareDialog({
  row,
  context,
}: {
  row: VersionRow;
  context: VersionTableContext;
}) {
  const isDefault = row.version === context.defaultVersion;
  const defaultText =
    context.defaultBody === undefined ? null : bodyText(context.defaultBody);
  const text = bodyText(row.body);
  const diffed = !isDefault && defaultText !== null;
  return (
    <Dialog>
      <DialogTrigger asChild>
        <Button type="button" variant="ghost" size="sm">
          {diffed ? "Compare" : "View"}
          <span className="sr-only"> v{row.version}</span>
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Version {row.version}</DialogTitle>
          <DialogDescription>
            Written by {row.author}.{" "}
            {isDefault
              ? "The default."
              : `Against v${context.defaultVersion}, the default.`}
          </DialogDescription>
        </DialogHeader>
        {diffed ? (
          <JsonDiff
            before={defaultText}
            after={text}
            beforeLabel={`v${context.defaultVersion} (default)`}
            afterLabel={`v${row.version}`}
          />
        ) : (
          <PolicyBody body={row.body} />
        )}
      </DialogContent>
    </Dialog>
  );
}

function columns(
  context: VersionTableContext,
): ColumnDef<VersionRow, unknown>[] {
  return [
    {
      id: "version",
      header: "Version",
      accessorFn: (row) => row.version,
      cell: ({ row }) => (
        <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
          <span className="font-mono font-medium tabular-nums">
            v{row.original.version}
          </span>
          {row.original.version === context.defaultVersion && (
            <StatusDot
              tone={context.active ? "ok" : "neutral"}
              label={context.active ? "In use" : "Default"}
            />
          )}
        </span>
      ),
    },
    {
      id: "author",
      header: "Written by",
      accessorFn: (row) => row.author,
      cell: ({ row }) => (
        <span className="break-all font-mono text-table">
          {row.original.author}
        </span>
      ),
    },
    {
      id: "actions",
      header: () => <span className="sr-only">Actions</span>,
      enableSorting: false,
      cell: ({ row }) => (
        <span className="flex flex-wrap items-center justify-end gap-1">
          <CompareDialog row={row.original} context={context} />
          {context.canWrite &&
            row.original.version !== context.defaultVersion && (
              <MakeDefaultDialog
                org={context.org}
                policy={context.policy}
                current={context.defaultVersion}
                currentText={
                  context.defaultBody === undefined
                    ? null
                    : bodyText(context.defaultBody)
                }
                target={row.original.version}
                targetText={bodyText(row.original.body)}
                active={context.active}
              />
            )}
        </span>
      ),
    },
  ];
}

/**
 * A policy's versions, newest first. The default is marked, and every
 * other one can be compared with it and made the default, a rollback
 * where it is older.
 */
export function VersionTable({
  rows,
  context,
}: {
  rows: VersionRow[];
  context: VersionTableContext;
}) {
  return (
    <DataTable
      label="Versions"
      columns={columns(context)}
      data={[...rows].sort((a, b) => b.version - a.version)}
      getRowId={(row) => String(row.version)}
      empty={<EmptyState icon={History} title="No versions" />}
    />
  );
}
