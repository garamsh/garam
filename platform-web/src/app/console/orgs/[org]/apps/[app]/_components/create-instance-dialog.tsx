"use client";

import { Plus } from "lucide-react";
import { useActionState, useState } from "react";

import { FormField } from "@garam/design/components/form-field";
import { Refusal } from "@garam/design/components/refusal";
import { Button } from "@garam/design/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@garam/design/ui/dialog";
import { Input } from "@garam/design/ui/input";
import { Textarea } from "@garam/design/ui/textarea";
import {
  createInstance,
  type CreateInstanceState,
} from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";
import { refuseUnsent } from "@/lib/unsent-action";

import { RegistrationToken } from "../../_components/registration-token";

const initialState: CreateInstanceState = { status: "idle" };

// A call garam's answer never came back from stays in the dialog as a
// refusal rather than closing it as if cancelled (#1631).
const create = refuseUnsent(
  createInstance,
  "garam didn't answer, so the instance may or may not have been created. Check the instance list before trying again.",
);

/**
 * Records that the installation has one more instance and shows the
 * token it registers with, once (ADR-0192 decision 1). The kind is a
 * label for the person's own use, whatever they type: garam stores it
 * and never reads it.
 */
export function CreateInstanceDialog({
  org,
  installation,
}: {
  org: string;
  installation: string;
}) {
  const [open, setOpen] = useState(false);
  const hydrated = useHydrated();

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button disabled={!hydrated}>
          <Plus />
          Create an instance
        </Button>
      </DialogTrigger>
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-lg">
        <CreateForm
          org={org}
          installation={installation}
          onCancel={() => setOpen(false)}
          onDone={() => setOpen(false)}
        />
      </DialogContent>
    </Dialog>
  );
}

/**
 * The dialog's form, mounted only while the dialog is open, so a token
 * shown and a refusal both go when it closes.
 */
function CreateForm({
  org,
  installation,
  onCancel,
  onDone,
}: {
  org: string;
  installation: string;
  onCancel: () => void;
  onDone: () => void;
}) {
  const [state, formAction, pending] = useActionState(create, initialState);
  const hydrated = useHydrated();

  return (
    <>
      <form action={formAction} className="flex flex-col gap-5">
        <DialogHeader>
          <DialogTitle>Create an instance</DialogTitle>
          <DialogDescription>
            One running copy of the app. garam answers a token that registers
            its connector, once.
          </DialogDescription>
        </DialogHeader>
        <input type="hidden" name="org" value={org} />
        <input type="hidden" name="installation" value={installation} />
        <FormField
          label="Name"
          hint="Optional. What the instance is called. Defaults to its identifier."
        >
          {(control) => <Input {...control} name="name" disabled={!hydrated} />}
        </FormField>
        <FormField
          label="Kind"
          hint="Optional. What it runs on, as you say it. garam stores it and never reads it."
        >
          {(control) => (
            <Input
              {...control}
              name="kind"
              autoComplete="off"
              spellCheck={false}
              disabled={!hydrated}
            />
          )}
        </FormField>
        <FormField label="Description" hint="Optional.">
          {(control) => (
            <Textarea
              {...control}
              name="description"
              rows={2}
              disabled={!hydrated}
            />
          )}
        </FormField>
        {state.status === "error" && <Refusal>{state.message}</Refusal>}
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onCancel}>
            Cancel
          </Button>
          <Button type="submit" disabled={!hydrated || pending}>
            {pending ? "Creating…" : "Create instance"}
          </Button>
        </DialogFooter>
      </form>
      <RegistrationToken
        name={state.status === "created" ? state.name : ""}
        token={state.status === "created" ? state.registrationToken : null}
        expiresAt={state.status === "created" ? state.expiresAt : undefined}
        onDone={onDone}
      />
    </>
  );
}
