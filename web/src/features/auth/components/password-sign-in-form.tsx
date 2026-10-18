"use client";

import { useActionState, useState } from "react";

import { FormField } from "@/components/form-field";
import { Refusal } from "@/components/refusal";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { RETURN_PARAM } from "@/lib/return-path";

import { signInWithPassword, type SignInWithPasswordState } from "../actions";

const initialState: SignInWithPasswordState = { status: "idle" };

/**
 * Signs in with a username and a password (ADR-0041).
 *
 * The fields carry no rule of their own. A wrong password and a
 * username no account holds are one answer, so the form asks nothing
 * about either before sending them and renders the single refusal the
 * action returns above the button, marking neither field as the wrong
 * one (ADR-0041).
 *
 * `returnPath` is where the visitor was going before they were sent
 * here; the action lands there once signed in.
 */
export function PasswordSignInForm({
  returnPath = null,
}: {
  returnPath?: string | null;
}) {
  const [state, formAction, pending] = useActionState(
    signInWithPassword,
    initialState,
  );
  // React resets a form once its action has run, so the username is
  // held here: a refusal that cleared it would make a person type it
  // again to read the answer. The password is not held — the reset
  // leaves the field empty for the next attempt.
  const [username, setUsername] = useState("");

  return (
    <form action={formAction} className="flex w-full flex-col gap-4">
      {returnPath !== null && (
        <input type="hidden" name={RETURN_PARAM} value={returnPath} />
      )}
      <FormField label="Username">
        {(control) => (
          <Input
            {...control}
            name="username"
            required
            value={username}
            onChange={(event) => setUsername(event.target.value)}
            autoComplete="username"
            autoCapitalize="none"
            spellCheck={false}
          />
        )}
      </FormField>
      <FormField label="Password">
        {(control) => (
          <Input
            {...control}
            name="password"
            type="password"
            required
            autoComplete="current-password"
          />
        )}
      </FormField>
      {state.status === "error" && <Refusal>{state.message}</Refusal>}
      <Button type="submit" disabled={pending}>
        {pending ? "Signing in…" : "Sign in"}
      </Button>
    </form>
  );
}
