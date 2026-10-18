"use client";

import { Button } from "@/components/ui/button";
import {
  archivePolicy,
  type ArchivePolicyState,
} from "@/features/orgs/actions";

import { ActionDialog } from "../../../../../_components/action-dialog";

const initialState: ArchivePolicyState = { status: "idle" };

/**
 * Archives a policy from a dialog saying what stays: garam deletes no
 * policy, and an archived one can be activated again.
 */
export function ArchivePolicyDialog({
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
        <Button type="button" variant="outline" className="text-destructive">
          Archive policy
        </Button>
      }
      title={`Archive ${policyName}?`}
      description={
        <p>
          It stops taking part in checks. Its versions and attachments are kept,
          and it can be activated again.
        </p>
      }
      action={archivePolicy}
      initialState={initialState}
      fields={{ org, policy }}
      confirmLabel="Archive policy"
      pendingLabel="Archiving…"
      destructive
      done={(state) => state.status === "idle"}
      outcome={() => "Archived. It can be activated again."}
      area="lifecycle"
    />
  );
}
