"use client";

import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  deleteOperator,
  type DeleteOperatorState,
} from "@/features/orgs/actions";

const initialState: DeleteOperatorState = { status: "idle" };

/**
 * Deletes one operator's registration, which stops that operator
 * authenticating to garam (ADR-0070). What it ends is a machine
 * identity, so the button asks first and the confirmation says what
 * stops and what does not: a single click would put a running operator
 * behind a misclick, and a reader could take "delete" to reach into the
 * cluster, which nothing here does.
 *
 * `enrolled` decides which consequence is true. Deleting an operator
 * that holds a certificate refuses that certificate from the next
 * connection; deleting one that never obtained a certificate stops
 * nothing that was running and frees the identifier.
 */
export function DeleteOperatorForm({
  org,
  operator,
  name,
  enrolled,
}: {
  org: string;
  operator: string;
  name: string;
  enrolled: boolean;
}) {
  const [state, formAction, pending] = useActionState(
    deleteOperator,
    initialState,
  );
  const [confirming, setConfirming] = useState(false);

  return (
    <div className="flex flex-col gap-2">
      {confirming ? (
        <form action={formAction} className="flex max-w-2xl flex-col gap-3">
          <input type="hidden" name="org" value={org} />
          <input type="hidden" name="operator" value={operator} />
          <p className="text-sm wrap-anywhere">
            {enrolled
              ? `Delete ${name}? Its new connections to garam are refused. Work already running in its cluster does not stop, and agents it provisioned keep their own certificates.`
              : `Delete ${name}? It never obtained a certificate, so nothing stops. Its identifier becomes free to register again.`}
          </p>
          <div className="flex flex-wrap gap-2">
            <Button type="submit" variant="destructive" disabled={pending}>
              {pending ? "Deleting…" : "Confirm deletion"}
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
          <Button
            type="button"
            variant="outline"
            onClick={() => setConfirming(true)}
          >
            Delete operator
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
