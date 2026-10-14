"use client";

import { useActionState, useState } from "react";

import { FormField } from "@/components/form-field";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  addGroupMember,
  type AddGroupMemberState,
} from "@/features/orgs/actions";

import type { Candidate } from "./team-member";

const initialState: AddGroupMemberState = { status: "idle" };

/**
 * Adds a person or an agent of this organization to the team, chosen by
 * name under its type. Only people and agents the team does not already
 * hold are offered, so a member cannot be chosen twice.
 *
 * The select stands outside the `<form>` and its choice is submitted
 * from a hidden input. React resets a form after its action, and the
 * select returns to its first value on a reset of the form holding it,
 * while a hidden input's value is its default; so a refused choice is
 * still the one shown and sent when the form is submitted again.
 */
export function AddTeamMemberForm({
  org,
  team,
  candidates,
  unlisted,
  partlyListed = [],
}: {
  org: string;
  team: string;
  candidates: Candidate[];
  /** The kinds whose list garam refused the caller, as plural nouns. */
  unlisted: string[];
  /**
   * Refused lists whose kind another list still offers, as plural
   * nouns: some of that kind are offered and some may be missing.
   */
  partlyListed?: string[];
}) {
  const [state, formAction, pending] = useActionState(
    addGroupMember,
    initialState,
  );
  const [chosen, setChosen] = useState("");
  // Once added, the member is no longer offered, and the choice goes
  // back to the prompt rather than to whoever is listed first.
  const choice = candidates.some((candidate) => candidate.grn === chosen)
    ? chosen
    : "";
  const groups = [
    {
      label: "People",
      entries: candidates.filter((candidate) => candidate.kind === "person"),
    },
    {
      label: "Agents",
      entries: candidates.filter((candidate) => candidate.kind === "agent"),
    },
  ].filter((group) => group.entries.length > 0);

  const notes = [
    ...(unlisted.length > 0
      ? [
          `This organization's ${unlisted.join(" and ")} are not listed to you, so none of them can be chosen here.`,
        ]
      : []),
    ...(partlyListed.length > 0
      ? [
          `This organization's ${partlyListed.join(" and ")} are not listed to you, so some agents may be missing here.`,
        ]
      : []),
  ];

  return (
    <section
      aria-labelledby="add-member-title"
      className="flex flex-col gap-3 rounded-xl border bg-card p-4"
    >
      <h2 id="add-member-title" className="text-base">
        Add a member
      </h2>
      {candidates.length === 0 ? (
        <div className="flex flex-col gap-1 text-muted-foreground">
          {notes.map((note) => (
            <p key={note}>{note}</p>
          ))}
          <p>
            {notes.length > 0
              ? "No one you can list is outside this team."
              : "Every person and agent of this organization is already in this team."}
          </p>
        </div>
      ) : (
        <div className="flex flex-col gap-3 sm:flex-row sm:items-start">
          <FormField
            label="Person or agent"
            hint={notes.length > 0 ? notes.join(" ") : undefined}
            error={state.status === "error" ? state.message : undefined}
            className="min-w-0 sm:max-w-sm"
          >
            {(control) => (
              <Select value={choice} onValueChange={setChosen}>
                <SelectTrigger {...control} className="w-full">
                  <SelectValue placeholder="Choose who to add" />
                </SelectTrigger>
                <SelectContent>
                  {groups.map((group) => (
                    <SelectGroup key={group.label}>
                      <SelectLabel>{group.label}</SelectLabel>
                      {group.entries.map((candidate) => (
                        <SelectItem key={candidate.grn} value={candidate.grn}>
                          <span>{candidate.name}</span>{" "}
                          <span className="font-mono text-xs text-muted-foreground">
                            {candidate.identifier}
                          </span>
                          {candidate.note && (
                            <span className="text-muted-foreground">
                              {" "}
                              · {candidate.note}
                            </span>
                          )}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  ))}
                </SelectContent>
              </Select>
            )}
          </FormField>
          <form action={formAction} className="sm:mt-6">
            <input type="hidden" name="org" value={org} />
            <input type="hidden" name="group" value={team} />
            <input type="hidden" name="member" value={choice} />
            <Button type="submit" disabled={pending || choice === ""}>
              {pending ? "Adding…" : "Add to team"}
            </Button>
          </form>
        </div>
      )}
    </section>
  );
}
