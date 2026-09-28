"use client";

import { useActionState } from "react";

import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import {
  addGroupMember,
  type AddGroupMemberState,
} from "@/features/orgs/actions";

import type { Candidate } from "./team-member";

const initialState: AddGroupMemberState = { status: "idle" };

/**
 * Adds a person or an agent of this organization to the team, chosen by
 * name with its type beside it. Only people and agents the team does
 * not already hold are offered, so a member cannot be chosen twice.
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
  const people = candidates.filter((candidate) => candidate.kind === "person");
  const agents = candidates.filter((candidate) => candidate.kind === "agent");

  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-semibold">Add a member</h2>
      {unlisted.length > 0 && (
        <p className="text-sm text-muted-foreground">
          This organization&apos;s {unlisted.join(" and ")} are not listed to
          you, so none of them can be chosen here.
        </p>
      )}
      {partlyListed.length > 0 && (
        <p className="text-sm text-muted-foreground">
          This organization&apos;s {partlyListed.join(" and ")} are not listed
          to you, so some agents may be missing here.
        </p>
      )}
      {candidates.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          {unlisted.length > 0 || partlyListed.length > 0
            ? "No one you can list is outside this team."
            : "Every person and agent of this organization is already in this team."}
        </p>
      ) : (
        <form
          action={formAction}
          className="flex max-w-2xl flex-col gap-3 sm:flex-row sm:items-end"
        >
          <input type="hidden" name="org" value={org} />
          <input type="hidden" name="group" value={team} />
          <div className="flex min-w-0 flex-1 flex-col gap-2">
            <Label htmlFor="member">Person or agent</Label>
            <select
              id="member"
              name="member"
              required
              defaultValue=""
              className="h-9 w-full min-w-0 rounded-md border border-input bg-transparent px-3 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30"
            >
              <option value="" disabled>
                Choose who to add
              </option>
              {people.length > 0 && (
                <optgroup label="People">
                  {people.map((candidate) => (
                    <option key={candidate.grn} value={candidate.grn}>
                      {candidate.label} · Person
                    </option>
                  ))}
                </optgroup>
              )}
              {agents.length > 0 && (
                <optgroup label="Agents">
                  {agents.map((candidate) => (
                    <option key={candidate.grn} value={candidate.grn}>
                      {candidate.label} · Agent
                    </option>
                  ))}
                </optgroup>
              )}
            </select>
          </div>
          <Button type="submit" disabled={pending}>
            {pending ? "Adding…" : "Add to team"}
          </Button>
        </form>
      )}
      {state.status === "error" && (
        <p role="alert" className="text-sm text-destructive">
          {state.message}
        </p>
      )}
    </section>
  );
}
