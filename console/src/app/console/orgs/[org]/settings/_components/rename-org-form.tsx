"use client";

import { Check } from "lucide-react";
import { useActionState, useState } from "react";

import { FormField } from "@/components/form-field";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { updateOrg, type UpdateOrgState } from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";

const initialState: UpdateOrgState = { status: "idle" };

/**
 * The organization's name, which is what people see. It is not its
 * identity, so it may change; the identifier may not.
 */
export function RenameOrgForm({ org, name }: { org: string; name: string }) {
  const [state, formAction, pending] = useActionState(updateOrg, initialState);
  const hydrated = useHydrated();
  const [draft, setDraft] = useState(name);
  const empty = draft.trim() === "";

  return (
    <form action={formAction} className="flex max-w-xl flex-col gap-4">
      <input type="hidden" name="org" value={org} />
      <FormField
        label="Name"
        error={state.status === "error" ? state.message : undefined}
      >
        {(control) => (
          <Input
            {...control}
            name="name"
            required
            disabled={!hydrated}
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
          />
        )}
      </FormField>
      <div className="flex flex-wrap items-center gap-2">
        <Button type="submit" disabled={!hydrated || pending || empty}>
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
