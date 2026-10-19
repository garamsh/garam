"use client";

import { Plus } from "lucide-react";
import { useActionState, useState } from "react";

import { FormField } from "@/components/form-field";
import { Refusal } from "@/components/refusal";
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
import { Textarea } from "@/components/ui/textarea";
import {
  registerOperator,
  type RegisterOperatorState,
} from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";
import {
  parseOperatorDescription,
  parseOperatorIdentifier,
  parseOperatorName,
} from "@/lib/operator";
import type { Parsed } from "@/lib/parsed";

const initialState: RegisterOperatorState = { status: "idle" };

function errorOf(parsed: Parsed<string>): string | undefined {
  return parsed.status === "error" ? parsed.message : undefined;
}

/**
 * The first step of connecting an operator: registering the identity it
 * authenticates as (ADR-0045). It opens from the list rather than from a
 * route of its own, because an identifier may be `new` and that route
 * would take the operator's page (ADR-0074); the rest of the setup is
 * the operator's own `connect` page, which registering opens, since
 * registering issues nothing and the key is issued there (ADR-0117).
 */
export function ConnectOperatorDialog({
  org,
  defaultOpen = false,
}: {
  org: string;
  defaultOpen?: boolean;
}) {
  const [open, setOpen] = useState(defaultOpen);
  // A fresh form each time the dialog opens: a registration shown once
  // is not shown again.
  const [attempt, setAttempt] = useState(0);

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (next) setAttempt((count) => count + 1);
      }}
    >
      <DialogTrigger asChild>
        <Button>
          <Plus />
          Connect an operator
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-lg">
        <RegisterStep key={attempt} org={org} onCancel={() => setOpen(false)} />
      </DialogContent>
    </Dialog>
  );
}

function RegisterStep({
  org,
  onCancel,
}: {
  org: string;
  onCancel: () => void;
}) {
  const [state, formAction, pending] = useActionState(
    registerOperator,
    initialState,
  );
  const hydrated = useHydrated();
  const [identifier, setIdentifier] = useState("");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");

  // What the form can refuse without a round trip, each beside its own
  // field. An empty identifier is left to `required`, and an empty name
  // is the identifier's.
  const identifierError =
    identifier === ""
      ? undefined
      : errorOf(parseOperatorIdentifier(identifier));
  const nameError = name === "" ? undefined : errorOf(parseOperatorName(name));
  const descriptionError = errorOf(parseOperatorDescription(description));
  const invalid = Boolean(identifierError || nameError || descriptionError);

  return (
    <form action={formAction} className="flex flex-col gap-5">
      <DialogHeader>
        <DialogTitle>Connect an operator</DialogTitle>
        <DialogDescription>
          Step 1 of 4: register the identity it authenticates as. An Operator
          has two — its manager and its control service — and each is connected
          on its own.
        </DialogDescription>
      </DialogHeader>
      <input type="hidden" name="org" value={org} />
      <FormField
        label="Identifier"
        hint="Lowercase letters, digits and hyphens. It cannot be changed later."
        error={identifierError}
      >
        {(control) => (
          <Input
            {...control}
            name="identifier"
            required
            autoComplete="off"
            spellCheck={false}
            disabled={!hydrated}
            value={identifier}
            onChange={(event) => setIdentifier(event.target.value)}
            placeholder="field-manager"
            className="font-mono"
          />
        )}
      </FormField>
      <FormField
        label="Name"
        hint="Optional. Defaults to the identifier."
        error={nameError}
      >
        {(control) => (
          <Input
            {...control}
            name="name"
            disabled={!hydrated}
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder={identifier || "Field cluster manager"}
          />
        )}
      </FormField>
      <FormField label="Description" hint="Optional." error={descriptionError}>
        {(control) => (
          <Textarea
            {...control}
            name="description"
            rows={2}
            disabled={!hydrated}
            value={description}
            onChange={(event) => setDescription(event.target.value)}
          />
        )}
      </FormField>
      {state.status === "error" && <Refusal>{state.message}</Refusal>}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={!hydrated || pending || invalid}>
          {pending ? "Registering…" : "Register"}
        </Button>
      </DialogFooter>
    </form>
  );
}
