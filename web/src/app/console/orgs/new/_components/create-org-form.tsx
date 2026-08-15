"use client";

import { useActionState } from "react";

import { Button } from "@/components/ui/button";
import {
  createOrg,
  type CreateOrgState,
} from "@/features/orgs/api/create-org";

const initialState: CreateOrgState = { status: "idle" };

export function CreateOrgForm() {
  const [state, formAction, pending] = useActionState(
    createOrg,
    initialState,
  );

  return (
    <form action={formAction} className="flex max-w-sm flex-col gap-4">
      <div className="flex flex-col gap-2">
        <label htmlFor="name" className="text-sm font-medium">
          Name
        </label>
        <input
          id="name"
          name="name"
          required
          autoFocus
          className="h-9 rounded-md border border-input bg-background px-3 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
        />
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
