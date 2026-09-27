"use client";

import { useActionState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { updateGroup, type UpdateGroupState } from "@/features/orgs/actions";

const initialState: UpdateGroupState = { status: "idle" };

/** Renames the team; its identifier and its members stay as they are. */
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

  return (
    <form action={formAction} className="flex max-w-2xl flex-col gap-3">
      <input type="hidden" name="org" value={org} />
      <input type="hidden" name="group" value={team} />
      <Label htmlFor="team-name">Team name</Label>
      <div className="flex flex-col gap-3 sm:flex-row">
        <Input
          id="team-name"
          name="name"
          required
          defaultValue={name}
          className="max-w-sm"
        />
        <Button type="submit" variant="outline" disabled={pending}>
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
