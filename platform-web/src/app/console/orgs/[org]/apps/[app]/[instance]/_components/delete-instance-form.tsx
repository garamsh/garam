"use client";

import { Button } from "@garam/design/ui/button";
import {
  deleteInstance,
  type DeleteInstanceState,
} from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";

import { ActionDialog } from "../../../../../../_components/action-dialog";

const initialState: DeleteInstanceState = { status: "idle" };

/**
 * Deletes the organization's record of one instance, which stops it
 * authenticating to garam (ADR-0184 decision 7). Nothing in the instance
 * is touched: garam never dials it. The person types its identifier.
 */
export function DeleteInstanceForm({
  org,
  installation,
  instance,
  name,
}: {
  org: string;
  installation: string;
  instance: string;
  name: string;
}) {
  const hydrated = useHydrated();

  return (
    <ActionDialog
      trigger={
        <Button variant="destructive" disabled={!hydrated}>
          Delete instance
        </Button>
      }
      title={`Delete ${name}?`}
      description={
        <p>
          garam refuses its keys and its registration token. Whatever it runs
          does not stop.
        </p>
      }
      action={deleteInstance}
      initialState={initialState}
      fields={{ org, installation, instance }}
      command={{
        label: "Delete instance",
        consequence: "garam refuses its keys; whatever it runs does not stop.",
      }}
      confirmLabel="Delete instance"
      pendingLabel="Deleting…"
      destructive
      typed={{ phrase: instance, mono: true }}
      done={(state) => state.status !== "error"}
    />
  );
}
