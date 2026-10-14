"use client";

import { ArrowRight, ChevronRight, Plus } from "lucide-react";
import Link from "next/link";
import { useActionState, useState } from "react";

import { Banner } from "@/components/banner";
import { FormField } from "@/components/form-field";
import { SecretReveal } from "@/components/secret-reveal";
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
import { cn } from "@/lib/utils";

const initialState: RegisterOperatorState = { status: "idle" };

function errorOf(parsed: Parsed<string>): string | undefined {
  return parsed.status === "error" ? parsed.message : undefined;
}

/**
 * The first step of connecting an operator: registering the identity it
 * authenticates as (ADR-0045). It opens from the list rather than from a
 * route of its own, because an identifier may be `new` and that route
 * would take the operator's page (ADR-0074); the rest of the setup is
 * the operator's own `connect` page.
 *
 * Registering also answers a legacy credential and an enrollment token,
 * once. An operator on keys needs neither, so they sit behind a closed
 * disclosure rather than in the way of the next step.
 */
export function ConnectOperatorDialog({
  org,
  operatorsHref,
  defaultOpen = false,
}: {
  org: string;
  operatorsHref: string;
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
        <RegisterStep
          key={attempt}
          org={org}
          operatorsHref={operatorsHref}
          onCancel={() => setOpen(false)}
        />
      </DialogContent>
    </Dialog>
  );
}

function RegisterStep({
  org,
  operatorsHref,
  onCancel,
}: {
  org: string;
  operatorsHref: string;
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

  if (state.status === "registered") {
    return (
      <Registered
        name={state.name}
        connectHref={`${operatorsHref}/${state.operator}/connect`}
        credential={state.credential}
        token={state.token}
      />
    );
  }

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
      {state.status === "error" && (
        <p role="alert" className="text-destructive">
          {state.message}
        </p>
      )}
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

/**
 * What registering answered: the way on to the key, and the two legacy
 * secrets behind a closed disclosure for an operator not yet on keys.
 */
function Registered({
  name,
  connectHref,
  credential,
  token,
}: {
  name: string;
  connectHref: string;
  credential: string;
  token: string;
}) {
  const [legacyOpen, setLegacyOpen] = useState(false);
  const [shown, setShown] = useState<"credential" | "token" | null>(null);

  return (
    <div className="flex flex-col gap-5">
      <DialogHeader>
        <DialogTitle>Registered {name}</DialogTitle>
        <DialogDescription>
          Next, issue its key. The key is what the operator signs in to garam
          with.
        </DialogDescription>
      </DialogHeader>
      <Banner tone="ok" title="The operator is registered">
        It holds no key yet, so it cannot authenticate until you issue one.
      </Banner>
      <div className="flex flex-col gap-2 rounded-xl border px-4 py-3">
        <button
          type="button"
          aria-expanded={legacyOpen}
          aria-controls="legacy-secrets"
          onClick={() => setLegacyOpen((open) => !open)}
          className="-mx-1 flex items-center gap-1.5 rounded-md px-1 text-left font-medium"
        >
          <ChevronRight
            aria-hidden
            className={cn(
              "size-4 transition-transform",
              legacyOpen && "rotate-90",
            )}
          />
          Legacy credential and enrollment token
        </button>
        {legacyOpen && (
          <div id="legacy-secrets" className="flex flex-col gap-3 pl-5.5">
            <p className="text-muted-foreground">
              For an operator not yet on keys. Each is shown once and garam
              keeps no copy; an operator on keys needs neither.
            </p>
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => setShown("credential")}
              >
                Show credential
              </Button>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => setShown("token")}
              >
                Show enrollment token
              </Button>
            </div>
          </div>
        )}
      </div>
      <SecretReveal
        open={shown === "credential"}
        title="Save the legacy credential"
        description="The operator exchanges it for short-lived tokens on garam's machine listener. It is shown once."
        secret={credential}
        noun="credential"
        onDone={() => setShown(null)}
      />
      <SecretReveal
        open={shown === "token"}
        title="Save the enrollment token"
        description="The operator presents it once to obtain its certificate. It is shown once."
        secret={token}
        noun="enrollment token"
        onDone={() => setShown(null)}
      />
      <DialogFooter>
        <Button asChild>
          <Link href={connectHref}>
            Continue: issue a key
            <ArrowRight />
          </Link>
        </Button>
      </DialogFooter>
    </div>
  );
}
