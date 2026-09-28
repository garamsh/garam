"use client";

import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { renameAgent, type RenameAgentState } from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";

const initialState: RenameAgentState = { status: "idle" };

/**
 * Renames the agent. Its identifier, operator and values stay as they
 * are, and no operator is told: the name is the organization's word for
 * the agent and not a setting (ADR-0075).
 */
export function RenameAgentForm({
  org,
  agent,
  name,
}: {
  org: string;
  agent: string;
  name: string;
}) {
  const [state, formAction, pending] = useActionState(
    renameAgent,
    initialState,
  );
  const hydrated = useHydrated();
  // Controlled, so a refused rename keeps what was typed: a form action
  // resets an uncontrolled field when it completes, refusal included.
  const [value, setValue] = useState(name);

  return (
    <form action={formAction} className="flex max-w-2xl flex-col gap-3">
      <input type="hidden" name="org" value={org} />
      <input type="hidden" name="agent" value={agent} />
      <Label htmlFor="agent-name">Name</Label>
      <div className="flex flex-col gap-3 sm:flex-row">
        <Input
          id="agent-name"
          name="name"
          required
          disabled={!hydrated}
          value={value}
          onChange={(event) => setValue(event.target.value)}
          className="max-w-sm"
        />
        <Button type="submit" variant="outline" disabled={!hydrated || pending}>
          {pending ? "Saving…" : "Save name"}
        </Button>
      </div>
      {state.status === "error" && (
        <p role="alert" className="text-sm text-destructive">
          {state.message}
        </p>
      )}
      {state.status === "saved" && (
        <p role="status" className="text-sm text-muted-foreground">
          Renamed to {state.name}.
        </p>
      )}
    </form>
  );
}
