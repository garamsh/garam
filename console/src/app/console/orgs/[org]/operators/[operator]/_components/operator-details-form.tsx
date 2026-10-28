"use client";

import { Check } from "lucide-react";
import { useActionState, useState } from "react";

import { FormField } from "@garam/design/components/form-field";
import { Button } from "@garam/design/ui/button";
import { Input } from "@garam/design/ui/input";
import { Textarea } from "@garam/design/ui/textarea";
import {
  updateOperator,
  type UpdateOperatorState,
} from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";
import { parseOperatorDescription, parseOperatorName } from "@/lib/operator";

const initialState: UpdateOperatorState = { status: "idle" };

/**
 * The operator's name and description, which are what the organization
 * calls it and says it is for (ADR-0074). Neither is its identity, so
 * either may change; the identifier may not.
 */
export function OperatorDetailsForm({
  org,
  operator,
  name,
  description,
}: {
  org: string;
  operator: string;
  name: string;
  description: string;
}) {
  const [state, formAction, pending] = useActionState(
    updateOperator,
    initialState,
  );
  const hydrated = useHydrated();
  const [draftName, setDraftName] = useState(name);
  const [draftDescription, setDraftDescription] = useState(description);
  const parsedName = parseOperatorName(draftName);
  const parsedDescription = parseOperatorDescription(draftDescription);
  const nameError =
    parsedName.status === "error"
      ? parsedName.message
      : state.status === "error"
        ? state.message
        : undefined;
  const descriptionError =
    parsedDescription.status === "error"
      ? parsedDescription.message
      : undefined;

  return (
    <form action={formAction} className="flex max-w-xl flex-col gap-4">
      <input type="hidden" name="org" value={org} />
      <input type="hidden" name="operator" value={operator} />
      <FormField label="Name" error={nameError}>
        {(control) => (
          <Input
            {...control}
            name="name"
            required
            disabled={!hydrated}
            value={draftName}
            onChange={(event) => setDraftName(event.target.value)}
          />
        )}
      </FormField>
      <FormField
        label="Description"
        hint="What this operator is for. Optional."
        error={descriptionError}
      >
        {(control) => (
          <Textarea
            {...control}
            name="description"
            rows={3}
            disabled={!hydrated}
            value={draftDescription}
            onChange={(event) => setDraftDescription(event.target.value)}
          />
        )}
      </FormField>
      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="submit"
          disabled={
            !hydrated ||
            pending ||
            parsedName.status === "error" ||
            parsedDescription.status === "error"
          }
        >
          {pending ? "Saving…" : "Save"}
        </Button>
        {state.status === "saved" && (
          <span
            role="status"
            className="flex items-center gap-1 text-muted-foreground"
          >
            <Check aria-hidden className="size-4 text-ok" />
            Saved as {state.name}
          </span>
        )}
      </div>
    </form>
  );
}
