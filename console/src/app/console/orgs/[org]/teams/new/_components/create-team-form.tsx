"use client";

import Link from "next/link";
import { useActionState, useState } from "react";

import { FormField } from "@garam/design/components/form-field";
import { Button } from "@garam/design/ui/button";
import { Input } from "@garam/design/ui/input";
import { createGroup, type CreateGroupState } from "@/features/orgs/actions";

import { orgHref } from "../../../../../_components/navigation";

const initialState: CreateGroupState = { status: "idle" };

/**
 * Asks for a team's name and nothing else: the roles a team grants are
 * optional on the API, and a field for them here would put a decision
 * in front of someone who came to make a team. A created team opens
 * its own page, so success renders nothing here; a refusal is the name
 * field's error. The field is controlled so that the reset React runs
 * after a refused action leaves the name typed in it.
 */
export function CreateTeamForm({ org }: { org: string }) {
  const [state, formAction, pending] = useActionState(
    createGroup,
    initialState,
  );
  const [name, setName] = useState("");

  return (
    <form action={formAction} className="flex flex-col gap-4">
      <input type="hidden" name="org" value={org} />
      <FormField
        label="Team name"
        error={state.status === "error" ? state.message : undefined}
      >
        {(control) => (
          <Input
            {...control}
            name="name"
            required
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="Engineering"
            className="max-w-sm"
          />
        )}
      </FormField>
      <div className="flex flex-wrap gap-2">
        <Button type="submit" disabled={pending}>
          {pending ? "Creating…" : "Create team"}
        </Button>
        <Button asChild variant="ghost">
          <Link href={orgHref(org, "teams")}>Cancel</Link>
        </Button>
      </div>
    </form>
  );
}
