"use client";

import Link from "next/link";
import { useActionState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { createGroup, type CreateGroupState } from "@/features/orgs/actions";

import { orgHref } from "../../../../../_components/navigation";

const initialState: CreateGroupState = { status: "idle" };

/**
 * Asks for a team's name and nothing else: the roles a team grants are
 * optional on the API, and a field for them here would put a decision
 * in front of someone who came to make a team. A created team opens
 * its own page, so success renders nothing here.
 */
export function CreateTeamForm({ org }: { org: string }) {
  const [state, formAction, pending] = useActionState(
    createGroup,
    initialState,
  );

  return (
    <form action={formAction} className="flex max-w-2xl flex-col gap-6">
      <input type="hidden" name="org" value={org} />

      <div className="flex flex-col gap-2">
        <Label htmlFor="name">Team name</Label>
        <Input
          id="name"
          name="name"
          required
          placeholder="Engineering"
          className="max-w-sm"
        />
      </div>

      {state.status === "error" && (
        <p role="alert" className="text-sm text-destructive">
          {state.message}
        </p>
      )}

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
