"use client";

import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  reissueOperatorCredential,
  type ReissueOperatorCredentialState,
} from "@/features/orgs/actions";

import { SecretField } from "../../_components/secret-field";

const initialState: ReissueOperatorCredentialState = { status: "idle" };

/**
 * Re-issues an operator's credential (ADR-0100). What it ends is the
 * credential the operator runs on and every token exchanged for it, so
 * the button asks first and the confirmation says so: the operator is
 * refused from its next request until the new credential is placed in
 * its cluster.
 *
 * The new credential is answered once and rendered here once, with the
 * warning above it, as registration renders the first.
 */
export function ReissueCredentialForm({
  org,
  operator,
  name,
}: {
  org: string;
  operator: string;
  name: string;
}) {
  const [state, formAction, pending] = useActionState(
    reissueOperatorCredential,
    initialState,
  );
  const [confirming, setConfirming] = useState(false);

  if (state.status === "issued") {
    return (
      <div className="flex max-w-2xl flex-col gap-3 rounded-md border border-border p-4">
        <p className="text-sm text-destructive">
          This is the only time this credential is shown; garam keeps no copy.
          The previous one and its tokens no longer authenticate.
        </p>
        <SecretField
          value={state.credential}
          label="Credential"
          noun="credential"
        />
        <p className="text-sm text-muted-foreground">
          Place it in the operator&apos;s cluster in place of the previous one.
        </p>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-2">
      {confirming ? (
        <form action={formAction} className="flex max-w-2xl flex-col gap-3">
          <input type="hidden" name="org" value={org} />
          <input type="hidden" name="operator" value={operator} />
          <p className="text-sm wrap-anywhere">
            {`Re-issue ${name}'s credential? The one it holds and every token it exchanged for it stop authenticating at once, so it is refused until the new credential is placed in its cluster.`}
          </p>
          <div className="flex flex-wrap gap-2">
            <Button type="submit" variant="destructive" disabled={pending}>
              {pending ? "Re-issuing…" : "Confirm re-issue"}
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
            Re-issue credential
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
