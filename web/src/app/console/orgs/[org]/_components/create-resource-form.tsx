"use client";

import { useActionState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  createGroup,
  createProject,
  type CreateResourceState,
} from "@/features/orgs/actions";

const initialState: CreateResourceState = { status: "idle" };

/**
 * What each section calls the resource it creates, and the action
 * behind it. Groups and projects share a list because the API gives
 * them one shape (`resource-list.tsx`), and they share this form for
 * the same reason: what differs between the two is the words.
 */
const RESOURCES = {
  group: {
    action: createGroup,
    heading: "Create a group",
    label: "Group name",
    placeholder: "Engineering",
    pending: "Creating…",
    submit: "Create group",
  },
  project: {
    action: createProject,
    heading: "Create a project",
    label: "Project name",
    placeholder: "Frontend",
    pending: "Creating…",
    submit: "Create project",
  },
} as const;

/**
 * Creates a group or a project of the organization, named by whatever
 * the person types. Only the name is asked for: the API takes roles
 * with a create and both are optional, so a field for them here would
 * put a decision in front of someone who came to make a group.
 */
export function CreateResourceForm({
  org,
  kind,
}: {
  org: string;
  kind: keyof typeof RESOURCES;
}) {
  const resource = RESOURCES[kind];
  const [state, formAction, pending] = useActionState(
    resource.action,
    initialState,
  );

  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-semibold">{resource.heading}</h2>
      <form action={formAction} className="flex max-w-2xl flex-col gap-6">
        <input type="hidden" name="org" value={org} />

        <div className="flex flex-col gap-2">
          <Label htmlFor="name">{resource.label}</Label>
          <Input
            id="name"
            name="name"
            required
            placeholder={resource.placeholder}
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
            {pending ? resource.pending : resource.submit}
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
