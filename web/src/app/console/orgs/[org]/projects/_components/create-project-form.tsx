"use client";

import { useActionState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  createProject,
  type CreateProjectState,
} from "@/features/orgs/actions";

const initialState: CreateProjectState = { status: "idle" };

/**
 * Creates a project of the organization, named by whatever the person
 * types. Only the name is asked for: the API takes roles with a create
 * and they are optional, so a field for them here would put a decision
 * in front of someone who came to make a project.
 */
export function CreateProjectForm({ org }: { org: string }) {
  const [state, formAction, pending] = useActionState(
    createProject,
    initialState,
  );

  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-semibold">Create a project</h2>
      <form action={formAction} className="flex max-w-2xl flex-col gap-6">
        <input type="hidden" name="org" value={org} />

        <div className="flex flex-col gap-2">
          <Label htmlFor="name">Project name</Label>
          <Input
            id="name"
            name="name"
            required
            placeholder="Frontend"
            className="max-w-sm"
          />
        </div>

        {state.status === "error" && (
          <p role="alert" className="text-sm text-destructive">
            {state.message}
          </p>
        )}

        <div>
          <Button type="submit" disabled={pending}>
            {pending ? "Creating…" : "Create project"}
          </Button>
        </div>
      </form>

      {state.status === "created" && (
        <p className="text-sm text-muted-foreground">
          Created {state.name}. The API addresses it as{" "}
          <code className="font-mono">{state.identifier}</code>.
        </p>
      )}
    </section>
  );
}
