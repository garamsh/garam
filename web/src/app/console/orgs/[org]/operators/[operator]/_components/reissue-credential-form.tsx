"use client";

import { useState } from "react";

import { SecretReveal } from "@/components/secret-reveal";
import { Button } from "@/components/ui/button";
import {
  reissueOperatorCredential,
  type ReissueOperatorCredentialState,
} from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";

import { ActionDialog } from "../../../../../_components/action-dialog";

const initialState: ReissueOperatorCredentialState = { status: "idle" };

/**
 * Re-issues an operator's legacy credential (ADR-0100). What it ends is
 * the credential the operator may run on and every token exchanged for
 * it, so it asks first and says so; the new credential is shown once.
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
  const hydrated = useHydrated();
  const [issued, setIssued] = useState<string | null>(null);
  // Which issued credential the person has kept, so the reveal closes
  // for that answer and opens again for the next.
  const [kept, setKept] = useState<string | null>(null);

  return (
    <div className="flex flex-col items-start gap-3">
      <ActionDialog
        trigger={
          <Button variant="outline" disabled={!hydrated}>
            Re-issue credential
          </Button>
        }
        title={`Re-issue ${name}'s credential?`}
        description={
          <p>
            The current credential, and every token exchanged for it, stop
            working the moment you confirm. An operator still on it is refused
            until the new one is placed in its cluster.
          </p>
        }
        action={reissueOperatorCredential}
        initialState={initialState}
        fields={{ org, operator }}
        command={{
          label: "Re-issue credential",
          consequence:
            "The current credential and its tokens stop authenticating.",
        }}
        confirmLabel="Re-issue credential"
        pendingLabel="Re-issuing…"
        destructive
        done={(state) => state.status === "issued"}
        onAnswer={(state) => {
          if (state.status === "issued") setIssued(state.credential);
        }}
      />
      <SecretReveal
        open={issued !== null && kept !== issued}
        title="Save the new credential"
        description="The previous credential and its tokens no longer authenticate. This one is shown once."
        secret={issued ?? ""}
        noun="credential"
        onDone={() => setKept(issued)}
      />
    </div>
  );
}
