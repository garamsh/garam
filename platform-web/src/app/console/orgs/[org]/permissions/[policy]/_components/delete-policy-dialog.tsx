"use client";

import { Button } from "@garam/design/ui/button";
import { deletePolicy, type DeletePolicyState } from "@/features/orgs/actions";

import { ActionDialog } from "../../../../../_components/action-dialog";

const initialState: DeletePolicyState = { status: "idle" };

/**
 * Deletes the policy once its name is typed. Its versions go with it
 * and garam brings none back, so one click is not enough. A deleted
 * policy opens the list.
 */
export function DeletePolicyDialog({
  org,
  policy,
  policyName,
}: {
  org: string;
  policy: string;
  policyName: string;
}) {
  return (
    <ActionDialog
      trigger={
        <Button type="button" variant="destructive">
          Delete policy
        </Button>
      }
      title={`Delete ${policyName}?`}
      description={
        <p>
          This removes the policy with its versions, so it takes part in no
          decision. garam cannot bring it back.
        </p>
      }
      action={deletePolicy}
      initialState={initialState}
      fields={{ org, policy }}
      typed={{ phrase: policyName }}
      command={{
        label: "Delete policy",
        consequence: "Removes the policy and its versions.",
      }}
      confirmLabel="Delete policy"
      pendingLabel="Deleting…"
      destructive
      done={(state) => state.status !== "error"}
    />
  );
}
