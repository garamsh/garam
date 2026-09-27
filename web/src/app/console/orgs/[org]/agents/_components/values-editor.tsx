"use client";

import { Plus, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  AGENT_VALUES_MAX_ENTRIES,
  AGENT_VALUE_MAX_BYTES,
  valueBytes,
} from "@/lib/agent-definition";

/** One row of the values editor; `id` keeps React on the same row. */
export type ValueRow = { id: number; key: string; value: string };

/**
 * The editor's rows for a set of values, or one empty row where there
 * are none, so an empty editor still offers a place to type.
 */
export function rowsFrom(values: Record<string, string>): ValueRow[] {
  const entries = Object.entries(values);
  if (entries.length === 0) return [{ id: 0, key: "", value: "" }];
  return entries.map(([key, value], id) => ({ id, key, value }));
}

/**
 * Edits a free key/value map. garam interprets no key and the operator
 * that reads one is not garam's to speak for, so the editor names none
 * and accepts any. It submits one `key` and one `value` field per row.
 */
export function ValuesEditor({
  rows,
  onChange,
}: {
  rows: readonly ValueRow[];
  onChange: (rows: ValueRow[]) => void;
}) {
  const atCap = rows.length >= AGENT_VALUES_MAX_ENTRIES;

  function update(id: number, field: "key" | "value", next: string) {
    onChange(
      rows.map((row) => (row.id === id ? { ...row, [field]: next } : row)),
    );
  }

  return (
    <div className="flex flex-col gap-3">
      <ul className="flex flex-col gap-4">
        {rows.map((row, index) => (
          <li key={row.id} className="flex flex-col gap-2 sm:flex-row">
            <Input
              name="key"
              value={row.key}
              onChange={(event) => update(row.id, "key", event.target.value)}
              aria-label={`Key ${index + 1}`}
              className="font-mono sm:w-56"
            />
            <div className="flex min-w-0 flex-1 flex-col gap-1">
              <Textarea
                name="value"
                rows={2}
                value={row.value}
                onChange={(event) =>
                  update(row.id, "value", event.target.value)
                }
                aria-label={`Value ${index + 1}`}
                className="font-mono text-sm"
              />
              <p className="text-xs text-muted-foreground">
                {valueBytes(row.value)} of {AGENT_VALUE_MAX_BYTES} bytes
              </p>
            </div>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label={`Remove value ${index + 1}`}
              disabled={rows.length === 1}
              onClick={() =>
                onChange(rows.filter((candidate) => candidate.id !== row.id))
              }
            >
              <X />
            </Button>
          </li>
        ))}
      </ul>

      <div className="flex items-center gap-3">
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={atCap}
          onClick={() =>
            onChange([...rows, { id: nextId(rows), key: "", value: "" }])
          }
        >
          <Plus />
          Add value
        </Button>
        <p className="text-sm text-muted-foreground">
          {rows.length} of {AGENT_VALUES_MAX_ENTRIES} values
        </p>
      </div>
    </div>
  );
}

/** One past the highest row id, so a removed row's id is never reused. */
function nextId(rows: readonly ValueRow[]): number {
  return rows.reduce((highest, row) => Math.max(highest, row.id), -1) + 1;
}
