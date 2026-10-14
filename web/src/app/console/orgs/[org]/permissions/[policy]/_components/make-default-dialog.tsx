"use client";

import { JsonDiff } from "@/components/json-diff";
import { Button } from "@/components/ui/button";
import {
  rollbackPolicy,
  type RollbackPolicyState,
} from "@/features/orgs/actions";

import { ActionDialog } from "../../_components/action-dialog";
import type { OutcomeArea } from "../../_components/outcome";
import { ConditionNotice } from "../../_components/condition-notice";
import {
  OrgWideConfirmation,
  ScopeConfirmationNotice,
} from "../../_components/org-wide-confirmation";

const initialState: RollbackPolicyState = { status: "idle" };

/**
 * Makes one stored version the policy's default, from a dialog showing
 * what changes against the current one. An older one is a rollback and
 * says so. While the policy is active the change takes effect at once,
 * so the dialog states whom it reaches and, for the whole organization,
 * carries the checkbox activation does.
 */
export function MakeDefaultDialog({
  org,
  policy,
  current,
  currentText,
  target,
  targetText,
  active,
  reach,
  orgWide,
  conditioned,
  area = "versions",
}: {
  org: string;
  policy: string;
  current: number;
  /** The default version's document, or null where it was not read. */
  currentText: string | null;
  target: number;
  targetText: string;
  active: boolean;
  reach: string;
  orgWide: boolean;
  /** Whether the version it would evaluate carries a Condition. */
  conditioned: boolean;
  /** Where the page says the change landed. */
  area?: OutcomeArea;
}) {
  const rollback = target < current;
  const everyone = (state: RollbackPolicyState) =>
    (active && orgWide) ||
    state.status === "confirm-org-wide" ||
    state.status === "confirm-unknown-scope";
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
          The policy evaluates v{target} instead of v{current}.{" "}
          {active
            ? `This applies at once. ${reach}`
            : "Nothing changes until the policy is activated."}
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
      area={area}
    >
      {(state) => (
        <>
          {currentText !== null && (
            <JsonDiff
              before={currentText}
              after={targetText}
              beforeLabel={`v${current} (default)`}
              afterLabel={`v${target}`}
            />
          )}
          {conditioned && <ConditionNotice />}
          <ScopeConfirmationNotice state={state} />
          {everyone(state) && (
            <OrgWideConfirmation id={`default-${target}-org-wide`} />
          )}
        </>
      )}
    </ActionDialog>
  );
}
