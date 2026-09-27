"use client";

import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { attachPolicy, type AttachPolicyState } from "@/features/orgs/actions";

const initialState: AttachPolicyState = { status: "idle" };

/** One group of options in the choice, under a heading. */
export type ChoiceGroup = {
  label: string;
  options: { value: string; label: string }[];
};

/**
 * Attaches a policy to a subject, one of the two being fixed by the
 * page and the other chosen by name: a policy's page chooses a team,
 * person or agent, and a subject's view chooses a policy. Only what is
 * not attached yet is offered.
 */
export function AttachPolicyForm({
  org,
  fixed,
  choose,
  empty,
}: {
  org: string;
  fixed: { name: "policy" | "subject"; value: string };
  choose: { label: string; placeholder: string; groups: ChoiceGroup[] };
  /** What stands in place of the form when there is nothing to offer. */
  empty: string;
}) {
  const [state, formAction, pending] = useActionState(
    attachPolicy,
    initialState,
  );
  const [chosen, setChosen] = useState("");
  const chosenName = fixed.name === "policy" ? "subject" : "policy";
  const offered = choose.groups.filter((group) => group.options.length > 0);

  return (
    <div className="flex flex-col gap-3">
      {offered.length === 0 ? (
        <p className="text-sm text-muted-foreground">{empty}</p>
      ) : (
        <form
          action={formAction}
          className="flex max-w-2xl flex-col gap-3 sm:flex-row sm:items-end"
        >
          <input type="hidden" name="org" value={org} />
          <input type="hidden" name={fixed.name} value={fixed.value} />
          <div className="flex min-w-0 flex-1 flex-col gap-2">
            <Label htmlFor={`attach-${chosenName}`}>{choose.label}</Label>
            <select
              id={`attach-${chosenName}`}
              name={chosenName}
              required
              value={chosen}
              onChange={(event) => setChosen(event.target.value)}
              className="h-9 w-full min-w-0 rounded-md border border-input bg-transparent px-3 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30"
            >
              <option value="" disabled>
                {choose.placeholder}
              </option>
              {offered.map((group) => (
                <optgroup key={group.label} label={group.label}>
                  {group.options.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </optgroup>
              ))}
            </select>
          </div>
          <Button type="submit" disabled={pending}>
            {pending ? "Attaching…" : "Attach"}
          </Button>
        </form>
      )}
      {state.status === "error" && (
        <p role="alert" className="text-sm text-destructive">
          {state.message}
        </p>
      )}
    </div>
  );
}
