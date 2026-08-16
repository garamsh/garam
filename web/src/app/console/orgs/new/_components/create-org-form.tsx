"use client";

import { useActionState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  createOrg,
  type CreateOrgState,
} from "@/features/orgs/api/create-org";

const initialState: CreateOrgState = { status: "idle" };

/** Names a new organization and reports back what the action refused. */
export function CreateOrgForm() {
  const [state, formAction, pending] = useActionState(
    createOrg,
    initialState,
  );

  return (
    <form action={formAction} className="flex max-w-sm flex-col gap-4">
      <div className="flex flex-col gap-2">
        <Label htmlFor="name">Name</Label>
        <Input id="name" name="name" required autoFocus />
      </div>
      {state.status === "error" && (
        <p role="alert" className="text-sm text-destructive">
          {state.message}
        </p>
      )}
      <div>
        <Button type="submit" disabled={pending}>
          {pending ? "Creating…" : "Create organization"}
        </Button>
      </div>
    </form>
  );
}
