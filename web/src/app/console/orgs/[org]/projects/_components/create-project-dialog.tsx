"use client";

import { Plus } from "lucide-react";
import { useActionState, useState } from "react";

import { CopyId } from "@/components/copy-id";
import { FormField } from "@/components/form-field";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  createProject,
  type CreateProjectState,
} from "@/features/orgs/actions";

const initialState: CreateProjectState = { status: "idle" };

/**
 * The list's primary action: a dialog that creates a project, named by
 * whatever the person types. Only the name is asked for — the API takes
 * roles with a create and they are optional, so a field for them would
 * put a decision in front of someone who came to make a project.
 *
 * A refusal stays in the dialog, attached to the field, with the name
 * still typed. A project created closes it, and what was created is
 * said beside the button that opened it.
 */
export function CreateProjectDialog({ org }: { org: string }) {
  const [open, setOpen] = useState(false);
  // Controlled, so the reset React runs after the action keeps it.
  const [name, setName] = useState("");
  const [state, formAction, pending] = useActionState(
    async (previous: CreateProjectState, formData: FormData) => {
      const next = await createProject(previous, formData);
      if (next.status === "created") {
        setOpen(false);
        setName("");
      }
      return next;
    },
    initialState,
  );

  return (
    <div className="flex flex-wrap items-center justify-end gap-x-3 gap-y-1">
      {state.status === "created" && !open && (
        <p
          role="status"
          className="flex flex-wrap items-center gap-x-2 text-muted-foreground"
        >
          <span>Created {state.name}.</span>
          <CopyId value={state.identifier} label={state.name} />
        </p>
      )}
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogTrigger asChild>
          <Button>
            <Plus aria-hidden />
            Create project
          </Button>
        </DialogTrigger>
        <DialogContent>
          <form action={formAction} className="flex flex-col gap-4">
            <DialogHeader>
              <DialogTitle>Create a project</DialogTitle>
              <DialogDescription>
                An agent is created inside a project, and its address names the
                project it was created in.
              </DialogDescription>
            </DialogHeader>
            <input type="hidden" name="org" value={org} />
            <FormField
              label="Project name"
              error={state.status === "error" ? state.message : undefined}
            >
              {(control) => (
                <Input
                  {...control}
                  name="name"
                  required
                  autoComplete="off"
                  placeholder="Frontend"
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                />
              )}
            </FormField>
            <DialogFooter>
              <Button type="submit" disabled={pending}>
                {pending ? "Creating…" : "Create project"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
