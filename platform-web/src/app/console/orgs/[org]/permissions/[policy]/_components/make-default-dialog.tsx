"use client";

import { JsonDiff } from "@/components/json-diff";
import { Button } from "@garam/design/ui/button";
import {
  rollbackPolicy,
  type RollbackPolicyState,
} from "@/features/orgs/actions";

import { ActionDialog } from "../../../../../_components/action-dialog";

const initialState: RollbackPolicyState = { status: "idle" };

/**
 * Makes one stored version the policy's default, from a dialog showing
 * what changes against the current one. An older one is a rollback and
 * says so. While the policy is active the change decides at once.
 */
export function MakeDefaultDialog({
  org,
  policy,
  current,
  currentText,
  target,
  targetText,
  active,
}: {
  org: string;
  policy: string;
  current: number;
  /** The default version's text, or null where it holds none. */
  currentText: string | null;
  target: number;
  /** The chosen version's text, or null where it holds none. */
  targetText: string | null;
  active: boolean;
}) {
  const rollback = target < current;
  return (
    <ActionDialog
      trigger={
        <Button type="button" variant="outline" size="sm">
          {rollback ? `Roll back to v${target}` : `Make v${target} default`}
        </Button>
      }
      title={
        rollback ? `Roll back to v${target}?` : `Make v${target} the default?`
      }
      description={
        <p>
          {active
            ? `v${target} decides for this organization from now on.`
            : "Nothing applies until activated."}
        </p>
      }
      action={rollbackPolicy}
      initialState={initialState}
      fields={{ org, policy, version: target }}
      confirmLabel={
        rollback ? `Roll back to v${target}` : `Make v${target} default`
      }
      pendingLabel="Saving…"
      done={(state) => state.status === "idle"}
      outcome={() =>
        rollback
          ? `Rolled back to v${target}.`
          : `v${target} is now the default.`
      }
      area="versions"
    >
      {() =>
        currentText !== null &&
        targetText !== null && (
          <JsonDiff
            before={currentText}
            after={targetText}
            beforeLabel={`v${current} (default)`}
            afterLabel={`v${target}`}
          />
        )
      }
    </ActionDialog>
  );
}
