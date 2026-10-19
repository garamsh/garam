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
import type { AddGroupMemberState } from "@/features/orgs/actions";

import type { Candidate } from "@/features/orgs";

import { SCOPE_KIND, type MembershipScope } from "./scope";

const initialState: AddGroupMemberState = { status: "idle" };

/** A choice, and the answer on screen when it was made. */
type Pick = { grn: string; at: AddGroupMemberState };

/** Whom a submission named, and the answer on screen when it was sent. */
type Sent = { name: string; at: AddGroupMemberState };

/**
 * Adds a person or an agent of this organization to a team or a
 * project, chosen by name under its type. Only people and agents it
 * does not already hold are offered, so a member cannot be chosen
 * twice.
 *
 * The choice is sent from a hidden input in the form, so the form posts
 * before hydration as after it. React resets the form after its action,
 * and the select answers a reset with its empty default, which is no
 * choice and is ignored: a refused choice stays chosen. An addition
 * clears the choice and says whom it added beside the button.
 */
export function AddMemberForm({
  org,
  scope,
  candidates,
  unlisted,
}: {
  org: string;
  scope: MembershipScope;
  candidates: Candidate[];
  /** The kinds whose list garam refused the caller, as plural nouns. */
  unlisted: string[];
}) {
  const kind = SCOPE_KIND[scope.kind];
  const [state, formAction, pending] = useActionState(kind.add, initialState);
  const [chosen, setChosen] = useState<Pick | null>(null);
  const [sent, setSent] = useState<Sent | null>(null);
  // A choice holds while it is still offered and no addition has
  // answered since it was made; a refusal keeps it.
  const choice =
    chosen !== null &&
    candidates.some((candidate) => candidate.grn === chosen.grn) &&
    (state === chosen.at || state.status === "error")
      ? chosen.grn
      : "";
  // Read at submission: an added member is no longer offered.
  const added =
    sent !== null && state !== sent.at && state.status === "idle"
      ? sent.name
      : null;
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
              ? `No one you can list is outside this ${kind.noun}.`
              : `Every person and agent of this organization is already in this ${kind.noun}.`}
          </p>
        </div>
      ) : (
        <form
          action={formAction}
          onSubmit={() => {
            const name = candidates.find((c) => c.grn === choice)?.name;
            setSent(name === undefined ? null : { name, at: state });
          }}
          className="flex flex-col gap-3 sm:flex-row sm:items-start"
        >
          <input type="hidden" name="org" value={org} />
          <input type="hidden" name={kind.field} value={scope.id} />
          <input type="hidden" name="member" value={choice} />
          <FormField
            label="Person or agent"
            hint={notes.length > 0 ? notes.join(" ") : undefined}
            error={state.status === "error" ? state.message : undefined}
            className="min-w-0 sm:max-w-sm"
          >
            {(control) => (
              <Select
                value={choice}
                onValueChange={(value) => {
                  // The form's reset answers with the empty default; no
                  // option is empty, so that is not a choice.
                  if (value === "") return;
                  setChosen({ grn: value, at: state });
                }}
              >
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
          <div className="flex items-center gap-3 sm:pt-6">
            <Button type="submit" disabled={pending}>
              {pending ? "Adding…" : `Add to ${kind.noun}`}
            </Button>
            <span role="status" className="text-muted-foreground">
              {added !== null ? `Added ${added}.` : ""}
            </span>
          </div>
        </form>
      )}
    </section>
  );
}
