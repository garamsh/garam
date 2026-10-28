"use client";

import { Button } from "@garam/design/ui/button";
import { deletePolicy, type DeletePolicyState } from "@/features/orgs/actions";

import { ActionDialog } from "../../../../../_components/action-dialog";

const initialState: DeletePolicyState = { status: "idle" };

/**
 * Deletes the policy once its name is typed. Its versions and
 * attachments go with it and garam brings none back, so one click is
 * not enough. A deleted policy opens the list.
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
          This removes the policy with its versions and attachments, so it takes
          part in no check. garam cannot bring it back.
        </p>
      }
      action={deletePolicy}
      initialState={initialState}
      fields={{ org, policy }}
      typed={{ phrase: policyName }}
      command={{
        label: "Delete policy",
        consequence: "Removes the policy, its versions and attachments.",
      }}
      confirmLabel="Delete policy"
      pendingLabel="Deleting…"
      destructive
      done={(state) => state.status !== "error"}
    />
  );
}
