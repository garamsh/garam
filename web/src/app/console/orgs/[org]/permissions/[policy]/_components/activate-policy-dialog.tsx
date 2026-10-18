"use client";

import { Button } from "@/components/ui/button";
import {
  activatePolicy,
  type ActivatePolicyState,
} from "@/features/orgs/actions";

import { ActionDialog } from "../../../../../_components/action-dialog";
import { ConditionNotice } from "../../_components/condition-notice";
import {
  OrgWideConfirmation,
  ScopeConfirmationNotice,
} from "../../_components/org-wide-confirmation";

const initialState: ActivatePolicyState = { status: "idle" };

/** Whether an answer asks for the organization-wide confirmation. */
function asksForEveryone(state: ActivatePolicyState): boolean {
  return (
    state.status === "confirm-org-wide" ||
    state.status === "confirm-unknown-scope"
  );
}

/**
 * Activates a draft or archived policy from a dialog stating the
 * version it will evaluate and whom it reaches. Where that is the whole
 * organization, the dialog carries a checkbox the form is not sent
 * without; garam finding no attachment when the page showed some asks
 * for the same.
 */
export function ActivatePolicyDialog({
  org,
  policy,
  policyName,
  version,
  reach,
  orgWide,
  conditioned,
}: {
  org: string;
  policy: string;
  policyName: string;
  version: number;
  /** The sentence stating whom the policy reaches once active. */
  reach: string;
  /** Whether that may be everyone in this organization. */
  orgWide: boolean;
  /** Whether the version it would evaluate carries a Condition. */
  conditioned: boolean;
}) {
  const everyone = (state: ActivatePolicyState) =>
    orgWide || asksForEveryone(state);
  return (
    <ActionDialog
      trigger={<Button type="button">Activate</Button>}
      title={`Activate ${policyName}?`}
      description={
        <p>
          Version {version} takes part in every check it reaches. {reach}
        </p>
      }
      action={activatePolicy}
      initialState={initialState}
      fields={{ org, policy }}
      confirmLabel={(state) =>
        everyone(state) ? "Activate for everyone" : "Activate policy"
      }
      pendingLabel="Activating…"
      done={(state) => state.status === "idle"}
      outcome={() => `Activated: v${version} takes part in checks.`}
      area="lifecycle"
    >
      {(state) => (
        <>
          {conditioned && <ConditionNotice />}
          <ScopeConfirmationNotice state={state} />
          {everyone(state) && <OrgWideConfirmation id="activate-org-wide" />}
        </>
      )}
    </ActionDialog>
  );
}
