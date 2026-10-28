"use client";

import { useActionState, useState } from "react";

import { FormField } from "@/components/form-field";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { renameAgent, type RenameAgentState } from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";
import { AGENT_NAME_MAX_LENGTH } from "@/lib/agent-name";

const initialState: RenameAgentState = { status: "idle" };

/**
 * Renames the agent, or clears its name with the field left empty, in
 * which case it is shown by its identifier. Nothing else about the
 * agent moves and no operator is told: the name is the organization's
 * word for the agent and not a setting (ADR-0119). A refusal is the
 * name field's error; the rename's success is said beside its Save.
 */
export function RenameAgentForm({
  org,
  agent,
  name,
}: {
  org: string;
  agent: string;
  name: string | null;
}) {
  const [state, formAction, pending] = useActionState(
    renameAgent,
    initialState,
  );
  const hydrated = useHydrated();
  // Controlled, so a refused rename keeps what was typed: a form action
  // resets an uncontrolled field when it completes, refusal included.
  const [value, setValue] = useState(name ?? "");

  return (
    <form action={formAction} className="flex max-w-md flex-col gap-3">
      <input type="hidden" name="org" value={org} />
      <input type="hidden" name="agent" value={agent} />
      <FormField
        label="Name"
        hint="Leave it empty to show the agent by its identifier."
        error={state.status === "error" ? state.message : undefined}
      >
        {(control) => (
          <Input
            {...control}
            name="name"
            maxLength={AGENT_NAME_MAX_LENGTH}
            disabled={!hydrated}
            value={value}
            onChange={(event) => setValue(event.target.value)}
          />
        )}
      </FormField>
      <div className="flex flex-wrap items-center gap-3">
        <Button type="submit" variant="outline" disabled={!hydrated || pending}>
          {pending ? "Saving…" : "Save name"}
        </Button>
        {state.status === "saved" && (
          <p role="status" className="text-muted-foreground">
            {state.name === null
              ? "Name cleared."
              : `Renamed to ${state.name}.`}
          </p>
        )}
      </div>
    </form>
  );
}
