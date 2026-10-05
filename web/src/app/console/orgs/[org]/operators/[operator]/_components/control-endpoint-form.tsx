"use client";

import { useActionState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
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

  return (
    <form action={formAction} className="flex max-w-2xl flex-col gap-3">
      <input type="hidden" name="org" value={org} />
      <input type="hidden" name="operator" value={operator} />
      <p className="text-sm">
        {current === null ? (
          "No control endpoint is registered. The console reaches no control service for this operator."
        ) : (
          <>
            The console sends this operator&apos;s operations to{" "}
            <span className="font-mono break-all">{current}</span> and to no
            other address.
          </>
        )}
      </p>
      <div className="flex flex-col gap-2">
        <Label htmlFor="control-endpoint">Control endpoint</Label>
        <Input
          id="control-endpoint"
          name="url"
          type="url"
          placeholder="https://control.example.com"
          defaultValue={current ?? ""}
          disabled={!hydrated}
          className="max-w-xl"
        />
      </div>
      {state.status === "error" && (
        <p role="alert" className="text-sm text-destructive">
          {state.message}
        </p>
      )}
      <p className="text-sm text-muted-foreground">
        Changing it requires the delegation permission, not the one that renames
        the operator. Operations already handed over keep the operator they
        name.
      </p>
      <div className="flex flex-wrap gap-2">
        <Button
          type="submit"
          name="intent"
          value="set"
          variant="outline"
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
      </div>
    </form>
  );
}
