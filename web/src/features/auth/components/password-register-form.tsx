"use client";

import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useHydrated } from "@/hooks/use-hydrated";
import {
  PASSWORD_MIN_LENGTH,
  USERNAME_MAX_LENGTH,
} from "@/lib/password-credentials";

import {
  registerWithPassword,
  type RegisterWithPasswordState,
} from "../actions";

const initialState: RegisterWithPasswordState = { status: "idle" };

/**
 * Registers the account an invitation names (ADR-0041).
 *
 * Opened from an invitation link, the page already holds the token and
 * hands it in as `invitationToken`: it travels as a hidden field, and
 * nobody copies it by hand. Opened without one, the form asks for it.
 *
 * The hints state the bounds the action holds, so a person meets them
 * before submitting rather than after.
 */
export function PasswordRegisterForm({
  invitationToken: invited = null,
}: {
  invitationToken?: string | null;
}) {
  const [state, formAction, pending] = useActionState(
    registerWithPassword,
    initialState,
  );
  // React resets a form once its action has run, so the token and the
  // username are held here: a refusal that cleared them would cost a
  // person the invitation they pasted. The password is not held — the
  // reset leaves the field empty for the next attempt.
  const [invitationToken, setInvitationToken] = useState(invited ?? "");
  const [username, setUsername] = useState("");
  // The fields are controlled, so text typed before React hydrates is
  // replaced by the empty state it hydrates with; until then they take
  // none.
  const hydrated = useHydrated();

  return (
    <form action={formAction} className="flex w-full flex-col gap-4">
      {invited !== null ? (
        <input type="hidden" name="invitationToken" value={invited} />
      ) : (
        <div className="flex flex-col gap-2">
          <Label htmlFor="invitationToken">Invitation token</Label>
          <Input
            id="invitationToken"
            name="invitationToken"
            required
            disabled={!hydrated}
            value={invitationToken}
            onChange={(event) => setInvitationToken(event.target.value)}
            autoComplete="off"
            spellCheck={false}
            className="font-mono"
            aria-describedby="invitation-hint"
          />
          <p id="invitation-hint" className="text-sm text-muted-foreground">
            The last part of the invitation link you were sent. Opening the link
            itself fills this in.
          </p>
        </div>
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
          aria-describedby="username-hint"
        />
        <p id="username-hint" className="text-sm text-muted-foreground">
          Lowercase letters, digits and hyphens, at most {USERNAME_MAX_LENGTH}{" "}
          characters. It names the account and does not change.
        </p>
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="password">Password</Label>
        <Input
          id="password"
          name="password"
          type="password"
          required
          disabled={!hydrated}
          autoComplete="new-password"
          aria-describedby="password-hint"
        />
        <p id="password-hint" className="text-sm text-muted-foreground">
          At least {PASSWORD_MIN_LENGTH} characters.
        </p>
      </div>
      {state.status === "error" && (
        <p role="alert" className="text-sm text-destructive">
          {state.message}
        </p>
      )}
      <Button type="submit" disabled={!hydrated || pending}>
        {pending ? "Creating…" : "Create account"}
      </Button>
    </form>
  );
}
