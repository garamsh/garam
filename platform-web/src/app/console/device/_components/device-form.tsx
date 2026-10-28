"use client";

import Link from "next/link";
import { useActionState, useState } from "react";

import { Banner } from "@garam/design/components/banner";
import { FormField } from "@garam/design/components/form-field";
import { Button } from "@garam/design/ui/button";
import { Input } from "@garam/design/ui/input";
import {
  decideDeviceCode,
  type DecideDeviceCodeState,
} from "@/features/auth/actions";

const initialState: DecideDeviceCodeState = { status: "idle" };

/**
 * The code a `garam login` printed, and the decision on it (ADR-0120).
 * Approve is the page's one primary action; Deny ends the sign-in. Once
 * garam has the decision the form gives way to what happened, so a
 * code is never decided twice from one page.
 */
export function DeviceForm({ initialCode }: { initialCode: string }) {
  const [state, formAction, pending] = useActionState(
    decideDeviceCode,
    initialState,
  );
  const [code, setCode] = useState(initialCode);

  if (state.status === "approved") {
    return (
      <Banner tone="ok" title="The CLI is signed in">
        Return to your terminal.
      </Banner>
    );
  }
  if (state.status === "denied") {
    return (
      <div className="flex flex-col items-start gap-3">
        <Banner tone="info" title="Sign-in denied">
          The CLI was not signed in.
        </Banner>
        <Button asChild variant="ghost">
          <Link href="/console/orgs">Go to your organizations</Link>
        </Button>
      </div>
    );
  }

  return (
    <form action={formAction} className="flex max-w-sm flex-col gap-4">
      <FormField
        label="Code"
        error={state.status === "error" ? state.message : undefined}
      >
        {(control) => (
          <Input
            {...control}
            name="userCode"
            value={code}
            onChange={(event) => setCode(event.target.value)}
            autoComplete="off"
            autoCapitalize="characters"
            spellCheck={false}
            required
            className="font-mono tracking-widest uppercase"
          />
        )}
      </FormField>
      <div className="flex flex-wrap items-center gap-3">
        <Button
          type="submit"
          name="decision"
          value="approve"
          disabled={pending}
        >
          {pending ? "Saving…" : "Approve"}
        </Button>
        <Button
          type="submit"
          name="decision"
          value="deny"
          variant="outline"
          disabled={pending}
        >
          Deny
        </Button>
      </div>
    </form>
  );
}
