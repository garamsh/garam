"use client";

import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  updateOperator,
  type UpdateOperatorState,
} from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";
import { parseOperatorDescription, parseOperatorName } from "@/lib/operator";

const initialState: UpdateOperatorState = { status: "idle" };

/**
 * Edits what the organization calls the operator and what it says the
 * operator is for. The identifier is not a field: it is the identity
 * the operator's certificate names, and saving moves nothing the
 * operator authenticates or enrols by (ADR-0074).
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
  const problem =
    parsedName.status === "error"
      ? parsedName.message
      : parsedDescription.status === "error"
        ? parsedDescription.message
        : null;
  const message = problem ?? (state.status === "error" ? state.message : null);

  return (
    <form action={formAction} className="flex max-w-2xl flex-col gap-4">
      <input type="hidden" name="org" value={org} />
      <input type="hidden" name="operator" value={operator} />
      <div className="flex flex-col gap-2">
        <Label htmlFor="operator-name">Name</Label>
        <Input
          id="operator-name"
          name="name"
          required
          disabled={!hydrated}
          value={draftName}
          onChange={(event) => setDraftName(event.target.value)}
          className="max-w-sm"
        />
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="operator-description">Description</Label>
        <Textarea
          id="operator-description"
          name="description"
          rows={3}
          disabled={!hydrated}
          value={draftDescription}
          onChange={(event) => setDraftDescription(event.target.value)}
        />
      </div>
      {message && (
        <p role="alert" className="text-sm text-destructive">
          {message}
        </p>
      )}
      {state.status === "saved" && problem === null && (
        <p role="status" className="text-sm text-muted-foreground">
          Saved as {state.name}.
        </p>
      )}
      <p className="text-sm text-muted-foreground">
        Saving leaves the identifier, certificate and definitions as they are.
      </p>
      <div>
        <Button
          type="submit"
          variant="outline"
          disabled={!hydrated || pending || problem !== null}
        >
          {pending ? "Saving…" : "Save details"}
        </Button>
      </div>
    </form>
  );
}
