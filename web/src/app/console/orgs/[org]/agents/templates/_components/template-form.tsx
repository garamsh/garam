"use client";

import Link from "next/link";
import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  createAgentTemplate,
  updateAgentTemplate,
  type CreateAgentTemplateState,
  type UpdateAgentTemplateState,
} from "@/features/orgs/actions";
import { parseAgentValues } from "@/lib/agent-definition";
import {
  parseTemplateDescription,
  parseTemplateName,
} from "@/lib/agent-template";
import type { Parsed } from "@/lib/parsed";

import { rowsFrom, ValuesEditor } from "../../_components/values-editor";

/** What the form starts from: nothing, or the template being edited. */
type Start =
  | { mode: "create" }
  | {
      mode: "edit";
      template: string;
      name: string;
      description: string;
      values: Record<string, string>;
    };

type State = CreateAgentTemplateState | UpdateAgentTemplateState;

const initialState: State = { status: "idle" };

/**
 * Writes a template's name, what it is for, and the values an agent
 * may copy from it. A created template opens its own page; an edited
 * one stays here and says it was saved. Neither reaches an agent
 * already defined (ADR-0075).
 */
export function TemplateForm({
  org,
  cancelHref,
  start,
}: {
  org: string;
  cancelHref: string;
  start: Start;
}) {
  const [state, formAction, pending] = useActionState<State, FormData>(
    start.mode === "create" ? createAgentTemplate : updateAgentTemplate,
    initialState,
  );
  const edit = start.mode === "edit" ? start : null;
  const [name, setName] = useState(edit?.name ?? "");
  const [description, setDescription] = useState(edit?.description ?? "");
  const [rows, setRows] = useState(() => rowsFrom(edit?.values ?? {}));

  // An empty name is left to its own `required`, so an untouched form
  // says nothing.
  const problem =
    (name === "" ? null : errorOf(parseTemplateName(name))) ??
    errorOf(parseTemplateDescription(description)) ??
    errorOf(parseAgentValues(rows));
  const message = problem ?? (state.status === "error" ? state.message : null);

  return (
    <form action={formAction} className="flex max-w-2xl flex-col gap-6">
      <input type="hidden" name="org" value={org} />
      {edit && <input type="hidden" name="template" value={edit.template} />}

      <div className="flex flex-col gap-2">
        <Label htmlFor="template-name">Name</Label>
        <Input
          id="template-name"
          name="name"
          required
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder="Research assistant"
          className="max-w-sm"
        />
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor="template-description">Description</Label>
        <Textarea
          id="template-description"
          name="description"
          rows={3}
          value={description}
          onChange={(event) => setDescription(event.target.value)}
          aria-describedby="template-description-hint"
        />
        <p
          id="template-description-hint"
          className="text-sm text-muted-foreground"
        >
          Optional. What agents made from this template are for.
        </p>
      </div>

      <div className="flex flex-col gap-3">
        <div className="flex flex-col gap-1">
          <h2 className="text-sm font-medium">Values</h2>
          <p className="text-sm text-muted-foreground">
            Copied into a new agent, where they can still be changed. Later
            edits here do not reach agents already created. Never put a
            credential in a value.
          </p>
        </div>
        <ValuesEditor rows={rows} onChange={setRows} />
      </div>

      {message && (
        <p role="alert" className="text-sm text-destructive">
          {message}
        </p>
      )}
      {state.status === "saved" && (
        <p role="status" className="text-sm text-muted-foreground">
          Saved {state.name}.
        </p>
      )}

      <div className="flex flex-wrap gap-2">
        <Button type="submit" disabled={pending || problem !== null}>
          {submitLabel(start.mode, pending)}
        </Button>
        <Button asChild variant="ghost">
          <Link href={cancelHref}>
            {start.mode === "create" ? "Cancel" : "Back to templates"}
          </Link>
        </Button>
      </div>
    </form>
  );
}

function submitLabel(mode: Start["mode"], pending: boolean): string {
  if (mode === "create") return pending ? "Creating…" : "Create template";
  return pending ? "Saving…" : "Save template";
}

/** The refusal a parse carried, or null when it carried none. */
function errorOf<T>(parsed: Parsed<T>): string | null {
  return parsed.status === "error" ? parsed.message : null;
}
