"use client";

import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  archivePolicy,
  type ArchivePolicyState,
} from "@/features/orgs/actions";

const initialState: ArchivePolicyState = { status: "idle" };

/**
 * Archives a policy after a confirmation saying what stays: garam
 * deletes no policy, and an archived one can be activated again.
 */
export function ArchivePolicyForm({
  org,
  policy,
  policyName,
}: {
  org: string;
  policy: string;
  policyName: string;
}) {
  const [state, formAction, pending] = useActionState(
    archivePolicy,
    initialState,
  );
  const [confirming, setConfirming] = useState(false);

  return (
    <div className="flex flex-col gap-2">
      {confirming ? (
        <form action={formAction} className="flex max-w-2xl flex-col gap-3">
          <input type="hidden" name="org" value={org} />
          <input type="hidden" name="policy" value={policy} />
          <p className="text-sm">
            Archive {policyName}? It stops taking part in checks. Its versions
            and attachments are kept, and it can be activated again.
          </p>
          <div className="flex flex-wrap gap-2">
            <Button type="submit" variant="destructive" disabled={pending}>
              {pending ? "Archiving…" : "Archive policy"}
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
            Archive…
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
