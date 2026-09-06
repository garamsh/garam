"use client";

import { useActionState, useState } from "react";
import { Plus, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { defineAgent, type DefineAgentState } from "@/features/orgs/actions";
import {
  AGENT_VALUES_MAX_ENTRIES,
  AGENT_VALUE_MAX_BYTES,
  parseAgentValues,
  valueBytes,
} from "@/lib/agent-definition";
import { parseOperatorIdentifier } from "@/lib/operator";
import type { Parsed } from "@/lib/parsed";

const initialState: DefineAgentState = { status: "idle" };

/** One row of the values editor; `id` keeps React on the same row. */
type Row = { id: number; key: string; value: string };

const initialRows: Row[] = [{ id: 0, key: "", value: "" }];

/**
 * Defines an agent: the operator that is to run it, and the values that
 * operator constructs it from.
 *
 * The values are a free key/value map. garam interprets no key and the
 * operator that reads one is not garam's to speak for, so this form
 * names none and accepts any — a fixed set of fields here would decide
 * for the operator what its payload means.
 */
export function DefineAgentForm({ org }: { org: string }) {
  const [state, formAction, pending] = useActionState(
    defineAgent,
    initialState,
  );
  const [operator, setOperator] = useState("");
  const [rows, setRows] = useState(initialRows);

  // The refusal the form can make without a round trip. An empty
  // operator field is left to its own `required`, so an untouched form
  // says nothing.
  const problem =
    (operator === "" ? null : errorOf(parseOperatorIdentifier(operator))) ??
    errorOf(parseAgentValues(rows));
  const message = problem ?? (state.status === "error" ? state.message : null);
  const atCap = rows.length >= AGENT_VALUES_MAX_ENTRIES;

  function update(id: number, field: "key" | "value", next: string) {
    setRows((current) =>
      current.map((row) => (row.id === id ? { ...row, [field]: next } : row)),
    );
  }

  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-semibold">Define an agent</h2>
      <form action={formAction} className="flex max-w-2xl flex-col gap-6">
        <input type="hidden" name="org" value={org} />

        <div className="flex flex-col gap-2">
          <Label htmlFor="operator">Operator</Label>
          <Input
            id="operator"
            name="operator"
            required
            value={operator}
            onChange={(event) => setOperator(event.target.value)}
            className="max-w-sm font-mono"
            aria-describedby="operator-hint"
          />
          <p id="operator-hint" className="text-sm text-muted-foreground">
            The identifier of the operator that is to run this agent. It reads
            the definition and builds the agent in its own cluster; garam never
            dials it.
          </p>
        </div>

        <div className="flex flex-col gap-3">
          <div className="flex flex-col gap-1">
            <h3 className="text-sm font-medium">Values</h3>
            <p className="text-sm text-muted-foreground">
              What the operator constructs the agent from. garam stores the keys
              and the strings and interprets neither, so the keys are the
              operator&apos;s to choose. Never put a credential in a value —
              name where the secret lives, not what it is.
            </p>
          </div>

          <ul className="flex flex-col gap-4">
            {rows.map((row, index) => {
              const bytes = valueBytes(row.value);
              return (
                <li key={row.id} className="flex flex-col gap-2 sm:flex-row">
                  <Input
                    name="key"
                    value={row.key}
                    onChange={(event) =>
                      update(row.id, "key", event.target.value)
                    }
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
                      {bytes} of {AGENT_VALUE_MAX_BYTES} bytes
                    </p>
                  </div>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    aria-label={`Remove value ${index + 1}`}
                    disabled={rows.length === 1}
                    onClick={() =>
                      setRows((current) =>
                        current.filter((candidate) => candidate.id !== row.id),
                      )
                    }
                  >
                    <X />
                  </Button>
                </li>
              );
            })}
          </ul>

          <div className="flex items-center gap-3">
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={atCap}
              onClick={() =>
                setRows((current) => [
                  ...current,
                  { id: nextId(current), key: "", value: "" },
                ])
              }
            >
              <Plus />
              Add value
            </Button>
            <p className="text-sm text-muted-foreground">
              {rows.length} of {AGENT_VALUES_MAX_ENTRIES} values
              {atCap && " — a definition carries no more."}
            </p>
          </div>
        </div>

        {message && (
          <p role="alert" className="text-sm text-destructive">
            {message}
          </p>
        )}

        <div>
          <Button type="submit" disabled={pending || problem !== null}>
            {pending ? "Defining…" : "Define agent"}
          </Button>
        </div>
      </form>

      {state.status === "defined" && (
        <div className="flex max-w-2xl flex-col gap-2">
          <p className="text-sm text-muted-foreground">
            Defined. garam minted this identifier for the agent; the operator
            builds it and no agent answers under it until one does.
          </p>
          <Input
            readOnly
            value={state.agent}
            aria-label="Agent identifier"
            onFocus={(event) => event.target.select()}
            className="max-w-sm bg-muted font-mono text-xs"
          />
        </div>
      )}
    </section>
  );
}

/** The refusal a parse carried, or null when it carried none. */
function errorOf<T>(parsed: Parsed<T>): string | null {
  return parsed.status === "error" ? parsed.message : null;
}

/** One past the highest row id, so a removed row's id is never reused. */
function nextId(rows: readonly Row[]): number {
  return rows.reduce((highest, row) => Math.max(highest, row.id), -1) + 1;
}
