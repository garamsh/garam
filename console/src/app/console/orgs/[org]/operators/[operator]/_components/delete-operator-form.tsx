"use client";

import { Button } from "@garam/design/ui/button";
import {
  deleteOperator,
  type DeleteOperatorState,
} from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";

import { ActionDialog } from "../../../../../_components/action-dialog";

const initialState: DeleteOperatorState = { status: "idle" };

/**
 * Deletes one operator's registration, which stops that operator
 * authenticating to garam (ADR-0070). It ends a machine identity, so it
 * is slow on purpose: the person types the identifier, and the dialog
 * says what stops and what does not — nothing here reaches into the
 * cluster.
 */
export function DeleteOperatorForm({
  org,
  operator,
  name,
}: {
  org: string;
  operator: string;
  name: string;
}) {
  const hydrated = useHydrated();

  return (
    <ActionDialog
      trigger={
        <Button variant="destructive" disabled={!hydrated}>
          Delete operator
        </Button>
      }
      title={`Delete ${name}?`}
      action={deleteOperator}
      initialState={initialState}
      fields={{ org, operator }}
      command={{
        label: "Delete operator",
        consequence:
          "garam refuses its keys and credential; work in its cluster does not stop.",
      }}
      confirmLabel="Delete operator"
      pendingLabel="Deleting…"
      destructive
      typed={{ phrase: operator, mono: true }}
      done={(state) => state.status !== "error"}
    />
  );
}
