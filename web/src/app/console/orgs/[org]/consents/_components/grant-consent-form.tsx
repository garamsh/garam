"use client";

import { useActionState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  grantOrgConsent,
  type GrantOrgConsentState,
} from "@/features/orgs/actions";

const initialState: GrantOrgConsentState = { status: "idle" };

/**
 * Grants this organization's half of a pair, named by the identifier
 * the other organization is addressed by. The same form joins a half
 * the other side is already holding and opens one nobody has answered
 * yet; what it answers with is the pair's state, not this half's.
 */
export function GrantConsentForm({ org }: { org: string }) {
  const [state, formAction, pending] = useActionState(
    grantOrgConsent,
    initialState,
  );

  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-semibold">Grant consent</h2>
      <p className="max-w-2xl text-sm text-muted-foreground">
        Name the other organization by the identifier it is addressed by.
        Granting stands this organization&apos;s half of the pair and nothing
        more — messages cross once that organization has granted its own, and
        granting a half that already stands changes nothing.
      </p>
      <form action={formAction} className="flex max-w-2xl flex-col gap-6">
        <input type="hidden" name="org" value={org} />

        <div className="flex flex-col gap-2">
          <Label htmlFor="other">Other organization</Label>
          <Input
            id="other"
            name="other"
            required
            placeholder="globex"
            className="max-w-sm font-mono"
          />
        </div>

        {state.status === "error" && (
          <p role="alert" className="text-sm text-destructive">
            {state.message}
          </p>
        )}

        <div>
          <Button type="submit" disabled={pending}>
            {pending ? "Granting…" : "Grant consent"}
          </Button>
        </div>
      </form>

      {state.status === "granted" && (
        <p className="text-sm text-muted-foreground">
          {state.stands
            ? `Granted. Messages now cross between this organization and ${state.other}.`
            : `Granted this organization's half for ${state.other}. Messages stay refused until it grants its own.`}
        </p>
      )}
    </section>
  );
}
