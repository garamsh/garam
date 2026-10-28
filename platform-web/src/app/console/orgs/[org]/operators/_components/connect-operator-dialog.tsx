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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@garam/design/ui/select";
import { Textarea } from "@garam/design/ui/textarea";
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
import type { Parsed } from "@garam/platform-sdk-ts/parsed";
import { refuseUnsent } from "@/lib/unsent-action";

import type { OperatorIdentity } from "./operator-pairs";

const initialState: RegisterOperatorState = { status: "idle" };

/** The choice that registers a new Operator: its cluster manager and
 * control service together. A Select item cannot carry an empty value,
 * so this is not one. */
const NEW_OPERATOR = "new";

// A call garam's answer never came back from stays in the dialog as a
// refusal rather than closing it as if cancelled (#1631).
const register = refuseUnsent(
  registerOperator,
  "garam didn't answer, so the operator may or may not have been registered. Check the operator list before trying again.",
);

function errorOf(parsed: Parsed<string>): string | undefined {
  return parsed.status === "error" ? parsed.message : undefined;
}

/**
 * The first step of connecting an Operator: registering the identities
 * it authenticates as (ADR-0045). It opens from the list rather than
 * from a route of its own, because an identifier may be `new` and that
 * route would take an identity's page (ADR-0074); the rest of the setup
 * is the `connect` page of what was registered, which registering
 * opens, since registering issues nothing and the keys are issued there
 * (ADR-0117).
 *
 * It registers a new Operator's cluster manager and control service in
 * one write (ADR-0124), or the control service of one of `managers`:
 * the cluster managers no control service names yet, which is every
 * Operator connected before the two were registered together.
 * `defaultManager` is the GRN of the one chosen first.
 */
export function ConnectOperatorDialog({
  org,
  managers = [],
  defaultManager,
  defaultOpen = false,
}: {
  org: string;
  managers?: OperatorIdentity[];
  defaultManager?: string;
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
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-lg">
        <RegisterStep
          key={attempt}
          org={org}
          managers={managers}
          defaultManager={defaultManager}
          onCancel={() => setOpen(false)}
        />
      </DialogContent>
    </Dialog>
  );
}

function RegisterStep({
  org,
  managers,
  defaultManager,
  onCancel,
}: {
  org: string;
  managers: OperatorIdentity[];
  defaultManager?: string;
  onCancel: () => void;
}) {
  const [state, formAction, pending] = useActionState(register, initialState);
  const hydrated = useHydrated();
  const [identifier, setIdentifier] = useState("");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [controlService, setControlService] = useState("");
  const [role, setRole] = useState(
    managers.find((manager) => manager.grn === defaultManager)?.grn ??
      NEW_OPERATOR,
  );
  const pair = role === NEW_OPERATOR;

  // What the form can refuse without a round trip, each beside its own
  // field. An empty identifier is left to `required`, and an empty name
  // is the identifier's.
  const identifierError =
    identifier === ""
      ? undefined
      : errorOf(parseOperatorIdentifier(identifier));
  const controlServiceError =
    !pair || controlService === ""
      ? undefined
      : controlService === identifier
        ? "The control service needs an identifier of its own."
        : errorOf(parseOperatorIdentifier(controlService));
  const nameError = name === "" ? undefined : errorOf(parseOperatorName(name));
  const descriptionError = errorOf(parseOperatorDescription(description));
  const invalid = Boolean(
    identifierError || controlServiceError || nameError || descriptionError,
  );

  return (
    <form action={formAction} className="flex flex-col gap-5">
      <DialogHeader>
        <DialogTitle>Connect an operator</DialogTitle>
        <DialogDescription>
          Step 1 of 4: register its two identities, the cluster manager and the
          control service. Each gets its own key in the next step.
        </DialogDescription>
      </DialogHeader>
      <input type="hidden" name="org" value={org} />
      <input type="hidden" name="manager" value={pair ? "" : role} />
      {managers.length > 0 && (
        <FormField
          label="Register"
          hint="An Operator connected before both were registered together has no control service yet."
        >
          {(control) => (
            <Select
              value={role}
              disabled={!hydrated}
              onValueChange={(value) => {
                if (value !== "") setRole(value);
              }}
            >
              <SelectTrigger {...control} className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NEW_OPERATOR}>A new Operator</SelectItem>
                {managers.map((manager) => (
                  <SelectItem key={manager.grn} value={manager.grn}>
                    <span>The control service of {manager.name}</span>{" "}
                    <span className="font-mono text-xs text-muted-foreground">
                      {manager.identifier}
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </FormField>
      )}
      <FormField
        label={
          pair ? "Cluster manager identifier" : "Control service identifier"
        }
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
            placeholder={pair ? "field-manager" : "field-control"}
            className="font-mono"
          />
        )}
      </FormField>
      {pair && (
        <FormField
          label="Control service identifier"
          hint="The same rule, and different from the cluster manager's."
          error={controlServiceError}
        >
          {(control) => (
            <Input
              {...control}
              name="controlService"
              required
              autoComplete="off"
              spellCheck={false}
              disabled={!hydrated}
              value={controlService}
              onChange={(event) => setControlService(event.target.value)}
              placeholder={
                identifier ? `${identifier}-control` : "field-control"
              }
              className="font-mono"
            />
          )}
        </FormField>
      )}
      <FormField
        label="Name"
        hint={
          pair
            ? "Optional. What the Operator is called. Defaults to the cluster manager's identifier."
            : "Optional. Defaults to the identifier."
        }
        error={nameError}
      >
        {(control) => (
          <Input
            {...control}
            name="name"
            disabled={!hydrated}
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder={identifier || "Field cluster"}
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
