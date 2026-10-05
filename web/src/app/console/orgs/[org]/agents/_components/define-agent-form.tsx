"use client";

import Link from "next/link";
import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { defineAgent, type DefineAgentState } from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";
import { parseAgentName, parseAgentValues } from "@/lib/agent-definition";
import { parseOperatorIdentifier } from "@/lib/operator";
import type { Parsed } from "@/lib/parsed";

import { orgHref } from "../../../../_components/navigation";
import { rowsFrom, ValuesEditor } from "./values-editor";

const initialState: DefineAgentState = { status: "idle" };

/** An operator the form offers: its identifier and what it is called. */
export type OperatorChoice = { identifier: string; name: string };

/**
 * Defines an agent: what the organization calls it, the operator that
 * is to run it, and the values that operator constructs it from. A
 * defined agent opens its own page.
 *
 * The operator is chosen by name from those the organization registered
 * (ADR-0074). A name is not unique, so each is offered with its
 * identifier, which is what the definition stores. Where the list was
 * not answered to the caller, the identifier is typed instead.
 *
 * The form starts blank: garam's templates are retired read-only and
 * no longer a start (ADR-0088).
 */
export function DefineAgentForm({
  org,
  operators,
}: {
  org: string;
  /** The operators to choose from, or null where none were listed. */
  operators: OperatorChoice[] | null;
}) {
  const [state, formAction, pending] = useActionState(
    defineAgent,
    initialState,
  );
  const hydrated = useHydrated();
  const [name, setName] = useState("");
  const [operator, setOperator] = useState("");
  const [rows, setRows] = useState(() => rowsFrom({}));

  // The refusals the form can make without a round trip. An empty name
  // or operator is left to its own `required`, so an untouched form
  // says nothing.
  const problem =
    (name === "" ? null : errorOf(parseAgentName(name))) ??
    (operator === "" ? null : errorOf(parseOperatorIdentifier(operator))) ??
    errorOf(parseAgentValues(rows));
  const message = problem ?? (state.status === "error" ? state.message : null);

  return (
    <form action={formAction} className="flex max-w-2xl flex-col gap-6">
      <input type="hidden" name="org" value={org} />

      <div className="flex flex-col gap-2">
        <Label htmlFor="agent-name">Name</Label>
        <Input
          id="agent-name"
          name="name"
          required
          disabled={!hydrated}
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder="Research assistant"
          className="max-w-sm"
          aria-describedby="agent-name-hint"
        />
        <p id="agent-name-hint" className="text-sm text-muted-foreground">
          You can rename it later. garam gives it a fixed identifier.
        </p>
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor="operator">Operator</Label>
        {operators === null ? (
          <Input
            id="operator"
            name="operator"
            required
            disabled={!hydrated}
            value={operator}
            onChange={(event) => setOperator(event.target.value)}
            className="max-w-sm font-mono"
            aria-describedby="operator-hint"
          />
        ) : (
          <select
            id="operator"
            name="operator"
            required
            disabled={!hydrated}
            value={operator}
            onChange={(event) => setOperator(event.target.value)}
            aria-describedby="operator-hint"
            className="h-9 w-full max-w-sm min-w-0 rounded-md border border-input bg-transparent px-3 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30"
          >
            <option value="" disabled>
              {operators.length === 0
                ? "No operators registered"
                : "Choose an operator"}
            </option>
            {operators.map(({ identifier, name: operatorName }) => (
              <option key={identifier} value={identifier}>
                {operatorName === identifier
                  ? identifier
                  : `${operatorName} (${identifier})`}
              </option>
            ))}
          </select>
        )}
        <p id="operator-hint" className="text-sm text-muted-foreground">
          {operators === null
            ? "Type the operator's identifier. Operators are not listed to you."
            : "It builds the agent in its own cluster and reports back."}
        </p>
        {operators?.length === 0 && (
          <p className="text-sm">
            <Link
              href={orgHref(org, "operators")}
              className="underline underline-offset-4"
            >
              Register an operator
            </Link>{" "}
            first.
          </p>
        )}
      </div>

      <div className="flex flex-col gap-3">
        <div className="flex flex-col gap-1">
          <h2 className="text-sm font-medium">Advanced settings</h2>
          <p className="text-sm text-muted-foreground">
            Saved values; application is not reported. Never put a credential in
            a value.
          </p>
        </div>
        <ValuesEditor rows={rows} onChange={setRows} disabled={!hydrated} />
      </div>

      {message && (
        <p role="alert" className="text-sm text-destructive">
          {message}
        </p>
      )}

      <div className="flex flex-wrap gap-2">
        <Button
          type="submit"
          disabled={!hydrated || pending || problem !== null}
        >
          {pending ? "Creating…" : "Create agent"}
        </Button>
        <Button asChild variant="ghost">
          <Link href={orgHref(org, "agents")}>Cancel</Link>
        </Button>
      </div>
    </form>
  );
}

/** The refusal a parse carried, or null when it carried none. */
function errorOf<T>(parsed: Parsed<T>): string | null {
  return parsed.status === "error" ? parsed.message : null;
}
