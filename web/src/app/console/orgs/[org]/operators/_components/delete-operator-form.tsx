"use client";

import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  deleteOperator,
  type DeleteOperatorState,
} from "@/features/orgs/actions";

const initialState: DeleteOperatorState = { status: "idle" };

/**
 * Deletes one operator's record, which stops that operator
 * authenticating (ADR-0070). What it ends is a live machine identity,
 * so the button asks first and the confirmation says what stops: a
 * single click would put a running operator behind a misclick.
 *
 * `enrolled` decides which consequence is true. Deleting an operator
 * that holds a certificate refuses that certificate from the next
 * connection; deleting one that never obtained a certificate stops
 * nothing that was running and frees the identifier.
 */
export function DeleteOperatorForm({
  org,
  operator,
  enrolled,
}: {
  org: string;
  operator: string;
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
        <form action={formAction} className="flex flex-col gap-3">
          <input type="hidden" name="org" value={org} />
          <input type="hidden" name="operator" value={operator} />
          <p className="text-sm">
            {enrolled
              ? `${operator} stops authenticating at once: the certificate it holds is refused from its next connection, and garam does not bring the record back. Agents it provisioned keep their own certificates.`
              : `${operator} obtained no certificate, so nothing it runs stops. The record and its unspent enrollment token go, and the identifier is free to register again.`}
          </p>
          <div className="flex flex-wrap gap-2">
            <Button type="submit" variant="destructive" disabled={pending}>
              {pending ? "Deleting…" : "Confirm deletion"}
            </Button>
            <Button
              type="button"
              variant="ghost"
              disabled={pending}
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
            Delete
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
