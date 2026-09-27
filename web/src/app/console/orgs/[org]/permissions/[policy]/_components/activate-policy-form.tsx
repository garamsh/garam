"use client";

import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  activatePolicy,
  type ActivatePolicyState,
} from "@/features/orgs/actions";

import { OrgWideConfirmation } from "../../_components/org-wide-confirmation";

const initialState: ActivatePolicyState = { status: "idle" };

/**
 * Activates a draft or archived policy after a confirmation stating
 * the version it will evaluate and whom it reaches. Where that is the
 * whole organization, the confirmation carries a checkbox the form is
 * not sent without; garam finding no attachment when the page showed
 * some asks for the same.
 */
export function ActivatePolicyForm({
  org,
  policy,
  policyName,
  version,
  reach,
  orgWide,
}: {
  org: string;
  policy: string;
  policyName: string;
  version: number;
  /** The sentence stating whom the policy reaches once active. */
  reach: string;
  /** Whether that may be everyone in this organization. */
  orgWide: boolean;
}) {
  const [state, formAction, pending] = useActionState(
    activatePolicy,
    initialState,
  );
  const [confirming, setConfirming] = useState(false);
  const needsConfirmation = orgWide || state.status === "confirm-org-wide";

  return (
    <div className="flex flex-col gap-2">
      {confirming ? (
        <form action={formAction} className="flex max-w-2xl flex-col gap-3">
          <input type="hidden" name="org" value={org} />
          <input type="hidden" name="policy" value={policy} />
          <p className="text-sm">
            Activate {policyName} with version {version}? {reach}
          </p>
          {state.status === "confirm-org-wide" && (
            <p role="alert" className="text-sm text-destructive">
              It has no attachments now, so it would apply to everyone in this
              organization.
            </p>
          )}
          {needsConfirmation && <OrgWideConfirmation id="activate-org-wide" />}
          <div className="flex flex-wrap gap-2">
            <Button type="submit" disabled={pending}>
              {pending
                ? "Activating…"
                : needsConfirmation
                  ? "Activate for everyone"
                  : "Activate"}
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
        <div>
          <Button type="button" onClick={() => setConfirming(true)}>
            Activate…
          </Button>
        </div>
      )}
      {state.status === "error" && (
        <p role="alert" className="text-sm text-destructive">
          {state.message}
        </p>
      )}
    </div>
  );
}
