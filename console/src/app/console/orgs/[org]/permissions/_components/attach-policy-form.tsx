"use client";

import { useActionState, useState } from "react";

import { FormField } from "@garam/design/components/form-field";
import { Button } from "@garam/design/ui/button";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@garam/design/ui/select";
import { attachPolicy, type AttachPolicyState } from "@/features/orgs/actions";

const initialState: AttachPolicyState = { status: "idle" };

/** One group of options in the choice, under a heading. */
export type ChoiceGroup = {
  label: string;
  /** `name` is what a sentence calls the option, where the label adds to it. */
  options: { value: string; label: string; name?: string }[];
};

/**
 * Attaches a policy to a subject, one of the two being fixed by the
 * page and the other chosen by name: a policy's page chooses a team,
 * person or agent, and a subject's view chooses a policy. Only what is
 * not attached yet is offered.
 *
 * The choice is posted from a hidden input the form holds, so a
 * refusal leaves it chosen: React resets a form after its action, and
 * a controlled input keeps its value through that.
 */
export function AttachPolicyForm({
  org,
  fixed,
  choose,
  empty,
}: {
  org: string;
  /** The side the page holds, and what a sentence calls it. */
  fixed: { name: "policy" | "subject"; value: string; label: string };
  choose: { label: string; placeholder: string; groups: ChoiceGroup[] };
  /** What stands in place of the form when there is nothing to offer. */
  empty: string;
}) {
  const [chosen, setChosen] = useState("");
  // What the last write attached, said beside the button.
  const [chosenLabel, setChosenLabel] = useState<string | null>(null);
  const chosenName = fixed.name === "policy" ? "subject" : "policy";
  const offered = choose.groups.filter((group) => group.options.length > 0);
  const optionOf = (value: string) =>
    offered
      .flatMap((group) => group.options)
      .find((option) => option.value === value);
  const labelOf = (value: string) => optionOf(value)?.label ?? null;
  const [state, formAction, pending] = useActionState(
    async (previous: AttachPolicyState, formData: FormData) => {
      const next = await attachPolicy(previous, formData);
      if (next.status === "idle") {
        const option = optionOf(String(formData.get(chosenName)));
        setChosenLabel(
          option === undefined ? null : (option.name ?? option.label),
        );
      }
      return next;
    },
    initialState,
  );
  // Once attached, the choice is no longer offered, and it goes back to
  // the prompt rather than to whatever is listed first.
  const choice = labelOf(chosen) === null ? "" : chosen;

  if (offered.length === 0) {
    return <p className="text-muted-foreground">{empty}</p>;
  }
  return (
    <form
      action={formAction}
      className="flex flex-col gap-3 sm:flex-row sm:items-start"
    >
      <input type="hidden" name="org" value={org} />
      <input type="hidden" name={fixed.name} value={fixed.value} />
      <input type="hidden" name={chosenName} value={choice} />
      <FormField
        label={choose.label}
        error={state.status === "error" ? state.message : undefined}
        className="min-w-0 flex-1 sm:max-w-md"
      >
        {(control) => (
          <Select
            value={choice}
            onValueChange={(value) => {
              // Radix answers the form's reset after every action with
              // the empty default; no option is empty, so that is not
              // a choice, and a refused one stays chosen.
              if (value === "") return;
              setChosen(value);
              setChosenLabel(null);
            }}
          >
            <SelectTrigger {...control} className="w-full min-w-0">
              <SelectValue placeholder={choose.placeholder} />
            </SelectTrigger>
            <SelectContent position="popper">
              {offered.map((group) => (
                <SelectGroup key={group.label}>
                  <SelectLabel>{group.label}</SelectLabel>
                  {group.options.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              ))}
            </SelectContent>
          </Select>
        )}
      </FormField>
      <div className="flex items-center gap-3 sm:pt-6">
        <Button type="submit" disabled={pending || choice === ""}>
          {pending ? "Attaching…" : "Attach"}
        </Button>
        <span
          role="status"
          className="text-sm text-muted-foreground empty:hidden"
        >
          {chosenLabel !== null &&
            (fixed.name === "policy"
              ? `Attached to ${chosenLabel}.`
              : `Attached ${chosenLabel} to ${fixed.label}.`)}
        </span>
      </div>
    </form>
  );
}
