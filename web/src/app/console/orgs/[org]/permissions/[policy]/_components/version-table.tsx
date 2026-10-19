"use client";

import { History } from "lucide-react";

import { DataTable, type ColumnDef } from "@/components/data-table";
import { JsonDiff } from "@/components/json-diff";
import { StatusDot } from "@/components/status-dot";
import { Time } from "@/components/time";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";

import { EmptyState } from "../../../../../_components/empty-state";
import { MakeDefaultDialog } from "./make-default-dialog";

/** One stored version as the table shows it. */
export type VersionRow = {
  version: number;
  /** The document, as indented JSON. */
  text: string;
  savedAt: string;
  author: string;
  conditioned: boolean;
};

/** What every row's make-default control needs to know. */
export type VersionTableContext = {
  org: string;
  policy: string;
  defaultVersion: number;
  defaultText: string | null;
  active: boolean;
  reach: string;
  orgWide: boolean;
  /** Whether making a version the default is offered. */
  canUpdate: boolean;
};

/** One version's document, against the default where it is not it. */
function CompareDialog({
  row,
  context,
}: {
  row: VersionRow;
  context: VersionTableContext;
}) {
  const isDefault = row.version === context.defaultVersion;
  return (
    <Dialog>
      <DialogTrigger asChild>
        <Button type="button" variant="ghost" size="sm">
          {isDefault || context.defaultText === null ? "View" : "Compare"}
          <span className="sr-only"> v{row.version}</span>
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Version {row.version}</DialogTitle>
          <DialogDescription>
            Saved by {row.author}.{" "}
            {isDefault
              ? "This is the version the policy evaluates."
              : `Shown against v${context.defaultVersion}, the default.`}
          </DialogDescription>
        </DialogHeader>
        {isDefault || context.defaultText === null ? (
          <pre className="max-h-96 overflow-auto rounded-lg border bg-muted/30 p-3 font-mono text-table leading-[1.6]">
            {row.text}
          </pre>
        ) : (
          <JsonDiff
            before={context.defaultText}
            after={row.text}
            beforeLabel={`v${context.defaultVersion} (default)`}
            afterLabel={`v${row.version}`}
          />
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
              label={context.active ? "Default · in use" : "Default"}
            />
          )}
          {row.original.conditioned && (
            <StatusDot tone="warn" label="Has a Condition" />
          )}
        </span>
      ),
    },
    {
      id: "saved",
      header: "Saved",
      accessorFn: (row) => row.savedAt,
      enableGlobalFilter: false,
      cell: ({ row }) => (
        <span className="flex flex-col">
          <Time value={row.original.savedAt} />
          <span className="text-muted-foreground">
            by {row.original.author}
          </span>
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
          {context.canUpdate &&
            row.original.version !== context.defaultVersion && (
              <MakeDefaultDialog
                org={context.org}
                policy={context.policy}
                current={context.defaultVersion}
                currentText={context.defaultText}
                target={row.original.version}
                targetText={row.original.text}
                active={context.active}
                reach={context.reach}
                orgWide={context.orgWide}
                conditioned={row.original.conditioned}
              />
            )}
        </span>
      ),
    },
  ];
}

/**
 * A policy's versions, newest first. The default is marked, and every
 * other one can be compared with it and made the default — a rollback,
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
      empty={
        <EmptyState
          icon={History}
          title="No versions"
        />
      }
    />
  );
}
