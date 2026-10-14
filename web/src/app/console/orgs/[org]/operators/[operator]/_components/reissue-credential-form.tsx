"use client";

import { useActionState, useState } from "react";

import { SecretReveal } from "@/components/secret-reveal";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import {
  reissueOperatorCredential,
  type ReissueOperatorCredentialState,
} from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";

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
  const [state, formAction, pending] = useActionState(
    reissueOperatorCredential,
    initialState,
  );
  const hydrated = useHydrated();
  // Which issued credential the person has kept, so the reveal closes
  // for that answer and opens again for the next.
  const [kept, setKept] = useState<string | null>(null);
  const issued = state.status === "issued" ? state.credential : null;

  return (
    <div className="flex flex-col items-start gap-3">
      <AlertDialog>
        <AlertDialogTrigger asChild>
          <Button variant="outline" disabled={!hydrated || pending}>
            Re-issue credential
          </Button>
        </AlertDialogTrigger>
        <AlertDialogContent>
          <form action={formAction} className="contents">
            <input type="hidden" name="org" value={org} />
            <input type="hidden" name="operator" value={operator} />
            <AlertDialogHeader>
              <AlertDialogTitle>
                Re-issue {name}&apos;s credential?
              </AlertDialogTitle>
              <AlertDialogDescription>
                The current credential, and every token exchanged for it, stop
                working the moment you confirm. An operator still on it is
                refused until the new one is placed in its cluster.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              <AlertDialogAction type="submit" variant="destructive">
                Re-issue credential
              </AlertDialogAction>
            </AlertDialogFooter>
          </form>
        </AlertDialogContent>
      </AlertDialog>
      {state.status === "error" && (
        <p role="alert" className="text-destructive">
          {state.message}
        </p>
      )}
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
