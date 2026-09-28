"use client";

import { useActionState } from "react";

import { Button } from "@/components/ui/button";
import {
  mintOperatorEnrollment,
  type MintOperatorEnrollmentState,
} from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";

import { EnrollmentToken, ExpiresAt } from "../../_components/enrollment-token";

const initialState: MintOperatorEnrollmentState = { status: "idle" };

/**
 * Mints an operator that holds no certificate a new enrollment token,
 * which replaces its previous one (ADR-0067).
 *
 * The page renders this whatever the operator's state, and the control
 * decides what to show: a mint refused because the operator enrolled
 * re-reads the page as `enrolled`, and the refusal has to outlive that
 * re-read rather than go with the button.
 *
 * Nothing here says the operator took the token: minting succeeds
 * whether or not anything reads it.
 */
export function MintEnrollmentForm({
  org,
  operator,
  enrolled,
}: {
  org: string;
  operator: string;
  enrolled: boolean;
}) {
  const [state, formAction, pending] = useActionState(
    mintOperatorEnrollment,
    initialState,
  );
  const hydrated = useHydrated();

  if (state.status === "minted") {
    return (
      <EnrollmentToken title="New enrollment token" token={state.token}>
        <p className="text-sm text-muted-foreground">
          Expires at <ExpiresAt at={state.expiresAt} />. Replaces any previous
          token.
        </p>
        <p className="text-sm">
          Update the operator&rsquo;s enrollment token Secret.
        </p>
      </EnrollmentToken>
    );
  }
  if (state.status === "already-enrolled") {
    return (
      <p role="alert" className="max-w-2xl text-sm text-destructive">
        This operator is already enrolled. A new token cannot be issued.
      </p>
    );
  }
  if (enrolled) return null;

  return (
    <form action={formAction} className="flex max-w-2xl flex-col gap-3">
      <input type="hidden" name="org" value={org} />
      <input type="hidden" name="operator" value={operator} />
      <p className="text-sm text-muted-foreground">
        Lost or expired token? Issue a replacement.
      </p>
      <div>
        <Button type="submit" variant="outline" disabled={!hydrated || pending}>
          {pending ? "Issuing…" : "Issue replacement token"}
        </Button>
      </div>
      {state.status === "error" && (
        <p role="alert" className="text-sm text-destructive">
          {state.message}
        </p>
      )}
    </form>
  );
}
