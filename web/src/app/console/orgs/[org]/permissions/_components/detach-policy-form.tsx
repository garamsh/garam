"use client";

import Link from "next/link";
import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import { detachPolicy, type DetachPolicyState } from "@/features/orgs/actions";

const initialState: DetachPolicyState = { status: "idle" };

/**
 * Detaches a policy from one subject after a confirmation naming both.
 * garam keeps an active policy's last attachment, and the refusal says
 * what to do instead; `policyHref` links the policy's page from a page
 * that is not it.
 */
export function DetachPolicyForm({
  org,
  policy,
  subject,
  policyName,
  subjectLabel,
  policyHref,
}: {
  org: string;
  policy: string;
  subject: string;
  policyName: string;
  subjectLabel: string;
  policyHref?: string;
}) {
  const [state, formAction, pending] = useActionState(
    detachPolicy,
    initialState,
  );
  const [confirming, setConfirming] = useState(false);

  return (
    <div className="flex flex-col items-end gap-2">
      {confirming ? (
        <form action={formAction} className="flex max-w-sm flex-col gap-3">
          <input type="hidden" name="org" value={org} />
          <input type="hidden" name="policy" value={policy} />
          <input type="hidden" name="subject" value={subject} />
          <p className="text-sm">
            Detach {policyName} from {subjectLabel}?
          </p>
          <div className="flex flex-wrap gap-2">
            <Button type="submit" variant="destructive" disabled={pending}>
              {pending ? "Detaching…" : "Confirm detach"}
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
        <Button
          type="button"
          variant="outline"
          size="sm"
          aria-label={`Detach ${policyName} from ${subjectLabel}`}
          onClick={() => setConfirming(true)}
        >
          Detach
        </Button>
      )}
      {state.status === "last-attachment" && (
        <div role="alert" className="flex max-w-sm flex-col gap-1 text-sm">
          <p className="text-destructive">
            Not detached: this is the last attachment of an active policy.
            Without it, the policy would apply to everyone in this organization.
          </p>
          <p className="text-muted-foreground">
            Attach it to another team, person or agent first, or archive the
            policy.
          </p>
          {policyHref !== undefined && (
            <Link
              href={policyHref}
              className="font-medium underline underline-offset-4"
            >
              Open {policyName}
            </Link>
          )}
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
