"use client";

import { useActionState, useState } from "react";

import { FormField } from "@garam/design/components/form-field";
import { Refusal } from "@garam/design/components/refusal";
import { Button } from "@garam/design/ui/button";
import { Input } from "@garam/design/ui/input";
import {
  PASSWORD_MIN_LENGTH,
  USERNAME_MAX_LENGTH,
} from "@garam/platform-client/password-credentials";

import {
  registerWithPassword,
  type RegisterField,
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
 * before submitting rather than after. A refusal is shown under the
 * field it is about; one about a token the form holds hidden, or about
 * no field, is shown above the button.
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
  const error = state.status === "error" ? state : null;
  const shown: readonly RegisterField[] =
    invited !== null
      ? ["username", "password"]
      : ["invitationToken", "username", "password"];
  const errorFor = (field: RegisterField) =>
    error?.field === field ? error.message : undefined;

  return (
    <form action={formAction} className="flex w-full flex-col gap-4">
      {invited !== null ? (
        <input type="hidden" name="invitationToken" value={invited} />
      ) : (
        <FormField
          label="Invitation token"
          hint="The last part of the invitation link you were sent. Opening the link itself fills this in."
          error={errorFor("invitationToken")}
        >
          {(control) => (
            <Input
              {...control}
              name="invitationToken"
              required
              value={invitationToken}
              onChange={(event) => setInvitationToken(event.target.value)}
              autoComplete="off"
              spellCheck={false}
              className="font-mono"
            />
          )}
        </FormField>
      )}
      <FormField
        label="Username"
        hint={`Lowercase letters, digits and hyphens, at most ${USERNAME_MAX_LENGTH} characters. It names the account and does not change.`}
        error={errorFor("username")}
      >
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
      <FormField
        label="Password"
        hint={`At least ${PASSWORD_MIN_LENGTH} characters.`}
        error={errorFor("password")}
      >
        {(control) => (
          <Input
            {...control}
            name="password"
            type="password"
            required
            autoComplete="new-password"
          />
        )}
      </FormField>
      {error !== null &&
        (error.field === undefined || !shown.includes(error.field)) && (
          <Refusal>{error.message}</Refusal>
        )}
      <Button type="submit" disabled={pending}>
        {pending ? "Creating…" : "Create account"}
      </Button>
    </form>
  );
}
