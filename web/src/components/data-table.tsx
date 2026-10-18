"use client";

import {
  flexRender,
  getCoreRowModel,
  getFilteredRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  useReactTable,
  type ColumnDef,
  type RowSelectionState,
  type SortingState,
} from "@tanstack/react-table";
import { ArrowDown, ArrowUp, ChevronsUpDown, Search } from "lucide-react";
import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export type { ColumnDef } from "@tanstack/react-table";

const ARIA_SORT = { asc: "ascending", desc: "descending" } as const;

/**
 * A list of objects as a table: sortable by the columns that allow it,
 * searchable when `search` is given, and paged past `pageSize` rows.
 * `empty` is what stands in for a list with nothing in it; a list the
 * caller was refused is not empty and is the page's to say, so it never
 * reaches here as `[]`.
 *
 * With `selection`, each row carries a checkbox named by
 * `selection.name`, and `selection.actions` renders above the table
 * with the rows chosen, for acting on several at once.
 *
 * Column definitions carry functions, so they are declared in a client
 * module next to the page that uses them.
 */
export function DataTable<TData>({
  label,
  columns,
  data,
  getRowId,
  empty,
  search,
  pageSize = 25,
  density = "default",
  selection,
}: {
  /** What the table lists, as its accessible caption. */
  label: string;
  columns: ColumnDef<TData, unknown>[];
  data: TData[];
  getRowId?: (row: TData) => string;
  empty: React.ReactNode;
  /** Offers a search over every column's text. */
  search?: { placeholder: string };
  pageSize?: number;
  density?: "default" | "compact";
  selection?: {
    /** How a row's checkbox is named: "Select scout". */
    name: (row: TData) => string;
    actions: (selected: TData[], clear: () => void) => React.ReactNode;
  };
}) {
  const [sorting, setSorting] = useState<SortingState>([]);
  const [query, setQuery] = useState("");
  const [rowSelection, setRowSelection] = useState<RowSelectionState>({});
  // TanStack Table hands back one mutable instance whose methods read
  // fresh state, so the React Compiler leaves this component unmemoized
  // — which is what it needs, and nothing below is memoized either.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data,
    columns,
    getRowId,
    state: { sorting, globalFilter: query, rowSelection },
    enableRowSelection: selection !== undefined,
    onRowSelectionChange: setRowSelection,
    onSortingChange: setSorting,
    onGlobalFilterChange: setQuery,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    initialState: { pagination: { pageSize } },
  });

  if (data.length === 0) return <>{empty}</>;

  const rows = table.getRowModel().rows;
  const matched = table.getFilteredRowModel().rows.length;
  const { pageIndex } = table.getState().pagination;
  const first = pageIndex * pageSize + 1;
  const last = Math.min(first + pageSize - 1, matched);
  const selected = table.getSelectedRowModel().rows.map((row) => row.original);
  const span = columns.length + (selection ? 1 : 0);

  return (
    <div className="flex flex-col gap-3">
      {search && (
        <div className="relative max-w-xs">
          <Search
            aria-hidden
            className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground"
          />
          <Input
            type="search"
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              table.setPageIndex(0);
            }}
            placeholder={search.placeholder}
            aria-label={search.placeholder}
            className="pl-8"
          />
        </div>
      )}
      {selection && (
        <div className="flex min-h-8 flex-wrap items-center gap-2">
          <span
            className="text-muted-foreground tabular-nums"
            aria-live="polite"
          >
            {selected.length === 0
              ? "Select rows to act on several at once."
              : `${selected.length} selected`}
          </span>
          {selection.actions(selected, () => setRowSelection({}))}
        </div>
      )}
      <div>
        <Table data-density={density}>
          <caption className="sr-only">{label}</caption>
          <TableHeader>
            {table.getHeaderGroups().map((group) => (
              <TableRow key={group.id} className="hover:bg-transparent">
                {selection && (
                  <TableHead className="w-10 px-3">
                    <input
                      type="checkbox"
                      aria-label="Select every row shown"
                      checked={table.getIsAllPageRowsSelected()}
                      ref={(input) => {
                        if (input) {
                          input.indeterminate =
                            table.getIsSomePageRowsSelected();
                        }
                      }}
                      onChange={table.getToggleAllPageRowsSelectedHandler()}
                      className="size-4 align-middle accent-primary"
                    />
                  </TableHead>
                )}
                {group.headers.map((header) => {
                  const sorted = header.column.getIsSorted();
                  return (
                    <TableHead
                      key={header.id}
                      aria-sort={sorted ? ARIA_SORT[sorted] : undefined}
                      className="px-3 text-xs font-medium text-muted-foreground"
                    >
                      {header.isPlaceholder ? null : header.column.getCanSort() ? (
                        <button
                          type="button"
                          onClick={header.column.getToggleSortingHandler()}
                          className="-mx-1 inline-flex items-center gap-1 rounded-md px-1 py-0.5 outline-none hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50"
                        >
                          {flexRender(
                            header.column.columnDef.header,
                            header.getContext(),
                          )}
                          {sorted === "asc" ? (
                            <ArrowUp aria-hidden className="size-3.5" />
                          ) : sorted === "desc" ? (
                            <ArrowDown aria-hidden className="size-3.5" />
                          ) : (
                            <ChevronsUpDown
                              aria-hidden
                              className="size-3.5 opacity-50"
                            />
                          )}
                        </button>
                      ) : (
                        flexRender(
                          header.column.columnDef.header,
                          header.getContext(),
                        )
                      )}
                    </TableHead>
                  );
                })}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {rows.length === 0 ? (
              <TableRow className="hover:bg-transparent">
                <TableCell
                  colSpan={span}
                  className="h-24 text-center text-muted-foreground"
                >
                  Nothing matches “{query}”.
                </TableCell>
              </TableRow>
            ) : (
              rows.map((row) => (
                <TableRow
                  key={row.id}
                  data-state={row.getIsSelected() ? "selected" : undefined}
                >
                  {selection && (
                    <TableCell className="w-10 px-3">
                      <input
                        type="checkbox"
                        aria-label={selection.name(row.original)}
                        checked={row.getIsSelected()}
                        onChange={row.getToggleSelectedHandler()}
                        className="size-4 align-middle accent-primary"
                      />
                    </TableCell>
                  )}
                  {row.getVisibleCells().map((cell) => (
                    <TableCell key={cell.id} className="px-3">
                      {flexRender(
                        cell.column.columnDef.cell,
                        cell.getContext(),
                      )}
                    </TableCell>
                  ))}
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
      {matched > pageSize && (
        <nav
          aria-label={`${label} pages`}
          className="flex items-center justify-between gap-3 text-muted-foreground"
        >
          <span aria-live="polite">
            {first}–{last} of {matched}
          </span>
          <div className="flex gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => table.previousPage()}
              disabled={!table.getCanPreviousPage()}
            >
              Previous
            </Button>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => table.nextPage()}
              disabled={!table.getCanNextPage()}
            >
              Next
            </Button>
          </div>
        </nav>
      )}
    </div>
  );
}
