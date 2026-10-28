"use client";

import { useActionState, useState } from "react";

import { FormField } from "@garam/design/components/form-field";
import { Button } from "@garam/design/ui/button";
import { Input } from "@garam/design/ui/input";
import {
  updateProject,
  type UpdateProjectState,
} from "@/features/orgs/actions";

const initialState: UpdateProjectState = { status: "idle" };

/**
 * Renames the project; its identifier and its members stay as they
 * are.
 * The saved name is said beside the Save, and a refusal is the name
 * field's error. The field is controlled so that the reset React runs
 * after a refused action leaves the name typed in it.
 */
export function RenameProjectForm({
  org,
  project,
  name,
}: {
  org: string;
  project: string;
  name: string;
}) {
  const [state, formAction, pending] = useActionState(
    updateProject,
    initialState,
  );
  const [typed, setTyped] = useState(name);

  return (
    <form action={formAction} className="flex flex-col gap-4">
      <input type="hidden" name="org" value={org} />
      <input type="hidden" name="project" value={project} />
      <FormField
        label="Project name"
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
