"use client";

import { useId, useState } from "react";

import { SecretReveal } from "@/components/secret-reveal";
import { Time } from "@/components/time";
import { Button } from "@/components/ui/button";
import {
  mintOperatorEnrollment,
  type MintOperatorEnrollmentState,
} from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";

import { ActionDialog } from "../../../../../_components/action-dialog";

const initialState: MintOperatorEnrollmentState = { status: "idle" };

const LABEL = "Issue a new enrollment token";

/**
 * Mints the operator a fresh enrollment token, for one holding no
 * certificate whose token was lost or ran out (ADR-0067). Where that
 * does not apply, or the server denies it, the control stays on the
 * page, inactive, with the reason beside it (ADR-0113). The token is
 * shown once and held in this component's state alone, until it is
 * kept.
 */
export function MintEnrollmentForm({
  org,
  operator,
  name,
  blocked,
}: {
  org: string;
  operator: string;
  name: string;
  /** Why it cannot be issued now; null where it can. */
  blocked: string | null;
}) {
  const hydrated = useHydrated();
  const reasonId = useId();
  const [minted, setMinted] = useState<{
    token: string;
    expiresAt: string;
  } | null>(null);

  if (blocked !== null) {
    return (
      <div className="flex flex-col items-start gap-2">
        {/* Inactive rather than disabled, so it stays focusable and a
            screen reader reaches the reason with it. */}
        <Button
          type="button"
          variant="outline"
          aria-disabled="true"
          aria-describedby={reasonId}
          className="cursor-not-allowed opacity-50"
        >
          {LABEL}
        </Button>
        <p id={reasonId} className="text-muted-foreground">
          {blocked}
        </p>
      </div>
    );
  }

  return (
    <div className="flex flex-col items-start gap-3">
      <ActionDialog
        trigger={
          <Button variant="outline" disabled={!hydrated}>
            {LABEL}
          </Button>
        }
        title={`Issue ${name} a new enrollment token?`}
        description={<p>Any earlier token stops working.</p>}
        action={mintOperatorEnrollment}
        initialState={initialState}
        fields={{ org, operator }}
        confirmLabel="Issue token"
        pendingLabel="Issuing…"
        done={(state) => state.status === "minted"}
        onAnswer={(state) => {
          if (state.status === "minted") {
            setMinted({ token: state.token, expiresAt: state.expiresAt });
          }
        }}
      />
      <SecretReveal
        open={minted !== null}
        title="Save the enrollment token"
        description={
          <>
            Works once, until <Time value={minted?.expiresAt ?? ""} />. Shown
            only now.
          </>
        }
        secret={minted?.token ?? ""}
        noun="enrollment token"
        onDone={() => setMinted(null)}
      />
    </div>
  );
}
