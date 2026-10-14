"use client";

import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useHydrated } from "@/hooks/use-hydrated";
import { RETURN_PARAM } from "@/lib/return-path";

import { signInWithPassword, type SignInWithPasswordState } from "../actions";

const initialState: SignInWithPasswordState = { status: "idle" };

/**
 * Signs in with a username and a password (ADR-0041).
 *
 * The fields carry no rule of their own. A wrong password and a
 * username no account holds are one answer, so the form asks nothing
 * about either before sending them and renders the single refusal the
 * action returns.
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
  // The fields are controlled, so text typed before React hydrates is
  // replaced by the empty state it hydrates with; until then they take
  // none.
  const hydrated = useHydrated();

  return (
    <form action={formAction} className="flex w-full flex-col gap-4">
      {returnPath !== null && (
        <input type="hidden" name={RETURN_PARAM} value={returnPath} />
      )}
      <div className="flex flex-col gap-2">
        <Label htmlFor="username">Username</Label>
        <Input
          id="username"
          name="username"
          required
          disabled={!hydrated}
          value={username}
          onChange={(event) => setUsername(event.target.value)}
          autoComplete="username"
          autoCapitalize="none"
          spellCheck={false}
        />
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="password">Password</Label>
        <Input
          id="password"
          name="password"
          type="password"
          required
          disabled={!hydrated}
          autoComplete="current-password"
        />
      </div>
      {state.status === "error" && (
        <p role="alert" className="text-sm text-destructive">
          {state.message}
        </p>
      )}
      <Button type="submit" disabled={!hydrated || pending}>
        {pending ? "Signing in…" : "Sign in"}
      </Button>
    </form>
  );
}
