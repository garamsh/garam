"use client";

import { useActionState } from "react";

import { Button } from "@/components/ui/button";
import {
  grantOrgConsent,
  type GrantOrgConsentState,
} from "@/features/orgs/actions";

const initialState: GrantOrgConsentState = { status: "idle" };

/**
 * Grants this organization's half of a pair the other side is already
 * holding, from the card that shows it waiting: the other organization
 * is the card's, so nothing is typed. A refusal is said beside the
 * button; a grant re-renders the card as the pair now stands.
 */
export function GrantFromCard({ org, other }: { org: string; other: string }) {
  const [state, formAction, pending] = useActionState(
    grantOrgConsent,
    initialState,
  );
  return (
    <form
      action={formAction}
      className="flex flex-wrap items-center gap-x-3 gap-y-2"
    >
      <input type="hidden" name="org" value={org} />
      <input type="hidden" name="other" value={other} />
      <Button type="submit" disabled={pending}>
        {pending ? "Granting…" : "Grant consent"}
      </Button>
      {state.status === "error" && (
        <p role="alert" className="text-danger">
          {state.message}
        </p>
      )}
    </form>
  );
}
