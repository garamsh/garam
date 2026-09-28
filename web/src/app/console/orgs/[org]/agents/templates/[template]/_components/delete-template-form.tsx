"use client";

import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  deleteAgentTemplate,
  type DeleteAgentTemplateState,
} from "@/features/orgs/actions";

const initialState: DeleteAgentTemplateState = { status: "idle" };

/**
 * Deletes the template after a confirmation naming it. Agents created
 * from it keep what they copied, which the confirmation says, since
 * that is the question a person deleting one has.
 */
export function DeleteTemplateForm({
  org,
  template,
  name,
}: {
  org: string;
  template: string;
  name: string;
}) {
  const [state, formAction, pending] = useActionState(
    deleteAgentTemplate,
    initialState,
  );
  const [confirming, setConfirming] = useState(false);

  return (
    <div className="flex flex-col gap-2">
      {confirming ? (
        <form action={formAction} className="flex max-w-2xl flex-col gap-3">
          <input type="hidden" name="org" value={org} />
          <input type="hidden" name="template" value={template} />
          <p className="text-sm">
            Delete {name}? Agents created from it are not changed. garam cannot
            bring the template back.
          </p>
          <div className="flex flex-wrap gap-2">
            <Button type="submit" variant="destructive" disabled={pending}>
              {pending ? "Deleting…" : `Delete ${name}`}
            </Button>
            <Button
              type="button"
              variant="ghost"
              disabled={pending}
              autoFocus
              onClick={() => setConfirming(false)}
            >
              Cancel
            </Button>
          </div>
        </form>
      ) : (
        <div>
          <Button
            type="button"
            variant="outline"
            onClick={() => setConfirming(true)}
          >
            Delete template
          </Button>
        </div>
      )}
      {state.status === "error" && (
        <p role="alert" className="text-sm text-destructive">
          {state.message}
        </p>
      )}
    </div>
  );
}
