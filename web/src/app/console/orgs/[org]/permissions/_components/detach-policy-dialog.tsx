"use client";

import Link from "next/link";

import { Banner } from "@/components/banner";
import { Button } from "@/components/ui/button";
import { detachPolicy, type DetachPolicyState } from "@/features/orgs/actions";

import { ActionDialog } from "../../../../_components/action-dialog";

const initialState: DetachPolicyState = { status: "idle" };

/**
 * Detaches a policy from one subject after a dialog naming both. garam
 * keeps an active policy's last attachment, and the refusal says what
 * to do instead; `policyHref` links the policy's page from a page that
 * is not it.
 */
export function DetachPolicyDialog({
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
  return (
    <ActionDialog
      trigger={
        <Button
          type="button"
          variant="ghost"
          size="sm"
          aria-label={`Detach ${policyName} from ${subjectLabel}`}
        >
          Detach
        </Button>
      }
      title={`Detach ${policyName}?`}
      description={
        <p>
          {policyName} stops applying to {subjectLabel} through this attachment.
          Its other attachments are kept.
        </p>
      }
      action={detachPolicy}
      initialState={initialState}
      fields={{ org, policy, subject }}
      confirmLabel="Detach policy"
      pendingLabel="Detaching…"
      destructive
      done={(state) => state.status === "idle"}
      outcome={() => `Detached from ${subjectLabel}.`}
      area="attachments"
    >
      {(state) =>
        state.status === "last-attachment" && (
          <Banner
            tone="danger"
            title="Not detached: this is its last attachment"
          >
            <p>
              Without it, this active policy would apply to everyone in this
              organization. Attach it to another team, person or agent first, or
              archive the policy.
            </p>
            {policyHref !== undefined && (
              <Link
                href={policyHref}
                className="font-medium text-foreground underline underline-offset-4"
              >
                Open {policyName}
              </Link>
            )}
          </Banner>
        )
      }
    </ActionDialog>
  );
}
