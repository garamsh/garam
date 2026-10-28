"use client";

import { useActionState } from "react";

import { Refusal } from "@garam/design/components/refusal";
import { Button } from "@garam/design/ui/button";
import { Input } from "@garam/design/ui/input";
import { Label } from "@garam/design/ui/label";
import { createOrg, type CreateOrgState } from "@/features/orgs/actions";

const initialState: CreateOrgState = { status: "idle" };

/** Names a new organization and reports back what the action refused. */
export function CreateOrgForm() {
  const [state, formAction, pending] = useActionState(createOrg, initialState);

  return (
    <form action={formAction} className="flex max-w-sm flex-col gap-4">
      <div className="flex flex-col gap-2">
        <Label htmlFor="name">Name</Label>
        <Input id="name" name="name" required autoFocus />
      </div>
      {state.status === "error" && <Refusal>{state.message}</Refusal>}
      <div>
        <Button type="submit" disabled={pending}>
          {pending ? "Creating…" : "Create organization"}
        </Button>
      </div>
    </form>
  );
}
