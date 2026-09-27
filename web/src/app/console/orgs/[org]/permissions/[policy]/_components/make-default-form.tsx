"use client";

import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  rollbackPolicy,
  type RollbackPolicyState,
} from "@/features/orgs/actions";

import { ConditionNotice } from "../../_components/condition-notice";
import {
  OrgWideConfirmation,
  ScopeConfirmationNotice,
} from "../../_components/org-wide-confirmation";

const initialState: RollbackPolicyState = { status: "idle" };

/**
 * Makes one stored version the policy's default, after a confirmation
 * naming the current and the chosen version. An older one is a
 * rollback and says so. While the policy is active the change takes
 * effect at once, so the confirmation states whom it reaches and, for
 * the whole organization, carries the checkbox activation does.
 */
export function MakeDefaultForm({
  org,
  policy,
  current,
  target,
  active,
  reach,
  orgWide,
  conditioned,
}: {
  org: string;
  policy: string;
  current: number;
  target: number;
  active: boolean;
  reach: string;
  orgWide: boolean;
  /** Whether the version it would evaluate carries a Condition. */
  conditioned: boolean;
}) {
  const [state, formAction, pending] = useActionState(
    rollbackPolicy,
    initialState,
  );
  const [confirming, setConfirming] = useState(false);
  const rollback = target < current;
  const action = rollback
    ? `Roll back to version ${target}`
    : `Make version ${target} default`;
  const needsConfirmation =
    (active && orgWide) ||
    state.status === "confirm-org-wide" ||
    state.status === "confirm-unknown-scope";

  return (
    <div className="flex flex-col items-end gap-2">
      {confirming ? (
        <form action={formAction} className="flex max-w-sm flex-col gap-3">
          <input type="hidden" name="org" value={org} />
          <input type="hidden" name="policy" value={policy} />
          <input type="hidden" name="version" value={target} />
          <p className="text-sm">
            Evaluate version {target} instead of version {current}?{" "}
            {active
              ? `This applies at once. ${reach}`
              : "Nothing changes until the policy is activated."}
          </p>
          {conditioned && <ConditionNotice />}
          <ScopeConfirmationNotice state={state} />
          {needsConfirmation && (
            <OrgWideConfirmation id={`default-${target}-org-wide`} />
          )}
          <div className="flex flex-wrap gap-2">
            <Button type="submit" disabled={pending}>
              {pending ? "Saving…" : action}
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
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => setConfirming(true)}
        >
          {rollback ? `Roll back to v${target}` : "Make default"}
        </Button>
      )}
      {state.status === "error" && (
        <p role="alert" className="text-sm text-destructive">
          {state.message}
        </p>
      )}
    </div>
  );
}
