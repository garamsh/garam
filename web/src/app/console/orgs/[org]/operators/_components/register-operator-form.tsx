"use client";

import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  registerOperator,
  type RegisterOperatorState,
} from "@/features/orgs/actions";
import { parseOperatorIdentifier } from "@/lib/operator";

const initialState: RegisterOperatorState = { status: "idle" };

/**
 * Registers an operator: the identifier the organization addresses it
 * by, which is also the last segment of the GRN its certificate names.
 *
 * The identifier is typed rather than minted, because an operator that
 * already holds a certificate has to be registered under the GRN that
 * certificate carries.
 */
export function RegisterOperatorForm({ org }: { org: string }) {
  const [state, formAction, pending] = useActionState(
    registerOperator,
    initialState,
  );
  const [identifier, setIdentifier] = useState("");

  // The refusal the form can make without a round trip. An empty field
  // is left to its own `required`, so an untouched form says nothing.
  const parsed = parseOperatorIdentifier(identifier);
  const problem =
    identifier === "" || parsed.status !== "error" ? null : parsed.message;
  const message = problem ?? (state.status === "error" ? state.message : null);

  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-semibold">
        Register an operator
      </h2>
      <p className="max-w-2xl text-sm text-muted-foreground">
        Use the identifier the operator&apos;s certificate was issued for.
        Registering records that this organization has the operator; it issues
        nothing and stops nothing. A record cannot be removed, so a mistyped
        identifier stays in the list — a control that removed one would look
        like revoking an operator and would revoke nothing, because a handshake
        is checked against the certificate and this organization&apos;s
        certificate authority and never against this record. Short of expiry,
        what does stop an operator is deleting this organization: that takes the
        certificate authority with it, every certificate the authority signed
        fails the handshake from then on, and garam does not bring the authority
        back.
      </p>
      <form action={formAction} className="flex max-w-2xl flex-col gap-6">
        <input type="hidden" name="org" value={org} />

        <div className="flex flex-col gap-2">
          <Label htmlFor="identifier">Identifier</Label>
          <Input
            id="identifier"
            name="identifier"
            required
            value={identifier}
            onChange={(event) => setIdentifier(event.target.value)}
            placeholder="field-one"
            className="max-w-sm font-mono"
          />
        </div>

        {message && (
          <p role="alert" className="text-sm text-destructive">
            {message}
          </p>
        )}

        <div>
          <Button type="submit" disabled={pending || problem !== null}>
            {pending ? "Registering…" : "Register operator"}
          </Button>
        </div>
      </form>

      {state.status === "registered" && (
        <p className="text-sm text-muted-foreground">
          Registered {state.operator}. An agent definition may name it now.
        </p>
      )}
    </section>
  );
}
