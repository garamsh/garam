"use client";

import { Check } from "lucide-react";
import { useActionState, useState } from "react";

import { CopyField } from "@/components/copy-field";
import { FormField } from "@/components/form-field";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  saveControlEndpoint,
  type ControlEndpointState,
} from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";

const initialState: ControlEndpointState = { status: "idle" };

/**
 * Registers where this operator's control service answers the console,
 * or clears it (#1170). It is a form of its own and not a field of the
 * details form: moving where operation authority is sent is the
 * delegating act, which renaming the operator does not grant.
 */
export function ControlEndpointForm({
  org,
  operator,
  endpoint,
}: {
  org: string;
  operator: string;
  endpoint: string | undefined;
}) {
  const [state, formAction, pending] = useActionState(
    saveControlEndpoint,
    initialState,
  );
  const hydrated = useHydrated();
  const current =
    state.status === "saved" ? state.endpoint : (endpoint ?? null);
  // Held in state so a refused save keeps what was typed: React resets
  // an uncontrolled field to its default after the action.
  const [draft, setDraft] = useState(endpoint ?? "");

  return (
    <form action={formAction} className="flex max-w-xl flex-col gap-4">
      <input type="hidden" name="org" value={org} />
      <input type="hidden" name="operator" value={operator} />
      {current === null ? (
        <p className="text-muted-foreground">No endpoint.</p>
      ) : (
        <CopyField label="Current endpoint" value={current} />
      )}
      <FormField
        label="Endpoint"
        hint="An https address."
        error={state.status === "error" ? state.message : undefined}
      >
        {(control) => (
          <Input
            {...control}
            name="url"
            type="url"
            placeholder="https://control.example.com"
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            disabled={!hydrated}
            className="font-mono"
          />
        )}
      </FormField>
      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="submit"
          name="intent"
          value="set"
          disabled={!hydrated || pending}
        >
          Save endpoint
        </Button>
        {current !== null && (
          <Button
            type="submit"
            name="intent"
            value="clear"
            variant="outline"
            disabled={!hydrated || pending}
          >
            Clear endpoint
          </Button>
        )}
        {state.status === "saved" && (
          <span
            role="status"
            className="flex items-center gap-1 text-muted-foreground"
          >
            <Check aria-hidden className="size-4 text-ok" />
            {state.endpoint === null ? "Endpoint cleared" : "Endpoint saved"}
          </span>
        )}
      </div>
    </form>
  );
}
