"use client";

import { Button } from "@garam/design/ui/button";
import {
  activatePolicy,
  type ActivatePolicyState,
} from "@/features/orgs/actions";

import { ActionDialog } from "../../../../../_components/action-dialog";

const initialState: ActivatePolicyState = { status: "idle" };

/**
 * Activates a draft policy from a dialog stating the version it will
 * decide by. The policy service refuses a caller who may not, and its
 * answer stays in the dialog.
 */
export function ActivatePolicyDialog({
  org,
  policy,
  policyName,
  version,
}: {
  org: string;
  policy: string;
  policyName: string;
  version: number;
}) {
  return (
    <ActionDialog
      trigger={<Button type="button">Activate</Button>}
      title={`Activate ${policyName}?`}
      description={
        <p>{`v${version} decides for this organization from now on.`}</p>
      }
      action={activatePolicy}
      initialState={initialState}
      fields={{ org, policy }}
      command={{
        label: "Activate policy",
        consequence: `v${version} decides from now on.`,
      }}
      confirmLabel="Activate policy"
      pendingLabel="Activating…"
      done={(state) => state.status === "idle"}
      outcome={() => `Activated v${version}`}
      area="lifecycle"
    />
  );
}
