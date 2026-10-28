"use client";

import { useActionState, useState } from "react";

import { FormField } from "@/components/form-field";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { updateGroup, type UpdateGroupState } from "@/features/orgs/actions";

const initialState: UpdateGroupState = { status: "idle" };

/**
 * Renames the team; its identifier and its members stay as they are.
 * The saved name is said beside the Save, and a refusal is the name
 * field's error. The field is controlled so that the reset React runs
 * after a refused action leaves the name typed in it.
 */
export function RenameTeamForm({
  org,
  team,
  name,
}: {
  org: string;
  team: string;
  name: string;
}) {
  const [state, formAction, pending] = useActionState(
    updateGroup,
    initialState,
  );
  const [typed, setTyped] = useState(name);

  return (
    <form action={formAction} className="flex flex-col gap-4">
      <input type="hidden" name="org" value={org} />
      <input type="hidden" name="group" value={team} />
      <FormField
        label="Team name"
        error={state.status === "error" ? state.message : undefined}
      >
        {(control) => (
          <Input
            {...control}
            name="name"
            required
            value={typed}
            onChange={(event) => setTyped(event.target.value)}
            className="max-w-sm"
          />
        )}
      </FormField>
      <div className="flex flex-wrap items-center gap-3">
        <Button type="submit" disabled={pending}>
          {pending ? "Saving…" : "Save name"}
        </Button>
        <span role="status" className="text-muted-foreground">
          {state.status === "saved" ? `Renamed to ${state.name}.` : ""}
        </span>
      </div>
    </form>
  );
}
