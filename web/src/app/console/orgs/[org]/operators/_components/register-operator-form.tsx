"use client";

import Link from "next/link";
import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  registerOperator,
  type RegisterOperatorState,
} from "@/features/orgs/actions";
import {
  parseOperatorDescription,
  parseOperatorIdentifier,
  parseOperatorName,
} from "@/lib/operator";
import { useHydrated } from "@/hooks/use-hydrated";
import type { Parsed } from "@/lib/parsed";

import { orgHref } from "../../../../_components/navigation";
import { SecretField } from "./secret-field";

const initialState: RegisterOperatorState = { status: "idle" };

/**
 * Registers an operator: the identifier the organization addresses it
 * by, which is also the last segment of the GRN its certificate names,
 * and the name and description it is shown by (ADR-0074).
 *
 * The identifier is typed rather than minted, because an operator that
 * already holds a certificate has to be registered under the GRN that
 * certificate carries. It cannot change afterwards, so it is marked as
 * the identity; the name can, so it is optional and falls back to the
 * identifier.
 *
 * The enrollment token and the credential come back with the record
 * and are rendered once. Nothing can fetch either again — garam stores
 * their hashes — so the panel says so where the person reads each
 * rather than afterwards (ADR-0064, ADR-0100).
 *
 * Only what has to be read before typing stands above the field: what
 * registering does. What the operator does with the token is beside
 * the token, and what deleting a record does is on the operator's own
 * page, beside the control that does it.
 *
 * It stands under the list rather than on a page of its own: an
 * identifier is typed, `new` is a valid one, and a create route would
 * take that operator's page (ADR-0074).
 */
export function RegisterOperatorForm({ org }: { org: string }) {
  const [state, formAction, pending] = useActionState(
    registerOperator,
    initialState,
  );
  const hydrated = useHydrated();
  const [identifier, setIdentifier] = useState("");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");

  // The refusal the form can make without a round trip. An empty
  // identifier is left to its own `required` and an empty name is the
  // identifier's, so an untouched form says nothing.
  const problem =
    (identifier === "" ? null : errorOf(parseOperatorIdentifier(identifier))) ??
    (name === "" ? null : errorOf(parseOperatorName(name))) ??
    errorOf(parseOperatorDescription(description));
  const message = problem ?? (state.status === "error" ? state.message : null);

  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-semibold">
        Register an operator
      </h2>
      <p className="max-w-2xl text-sm text-muted-foreground">
        Registering records that this organization has the operator and hands
        back the credential it authenticates with.
      </p>
      <form action={formAction} className="flex max-w-2xl flex-col gap-6">
        <input type="hidden" name="org" value={org} />

        <div className="flex flex-col gap-2">
          <Label htmlFor="identifier">Identifier</Label>
          <Input
            id="identifier"
            name="identifier"
            required
            disabled={!hydrated}
            value={identifier}
            onChange={(event) => setIdentifier(event.target.value)}
            placeholder="field-one"
            className="max-w-sm font-mono"
            aria-describedby="identifier-hint"
          />
          <p id="identifier-hint" className="text-sm text-muted-foreground">
            Its certificate identity. It cannot change.
          </p>
        </div>

        <div className="flex flex-col gap-2">
          <Label htmlFor="operator-name">Name</Label>
          <Input
            id="operator-name"
            name="name"
            disabled={!hydrated}
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder={identifier || "Field cluster one"}
            className="max-w-sm"
            aria-describedby="operator-name-hint"
          />
          <p id="operator-name-hint" className="text-sm text-muted-foreground">
            Optional. Defaults to the identifier.
          </p>
        </div>

        <div className="flex flex-col gap-2">
          <Label htmlFor="operator-description">Description</Label>
          <Textarea
            id="operator-description"
            name="description"
            rows={3}
            disabled={!hydrated}
            value={description}
            onChange={(event) => setDescription(event.target.value)}
          />
        </div>

        {message && (
          <p role="alert" className="text-sm text-destructive">
            {message}
          </p>
        )}

        <div>
          <Button
            type="submit"
            disabled={!hydrated || pending || problem !== null}
          >
            {pending ? "Registering…" : "Register operator"}
          </Button>
        </div>
      </form>

      {state.status === "registered" && (
        <Registered
          href={`${orgHref(org, "operators")}/${state.operator}`}
          name={state.name}
          token={state.token}
          expiresAt={state.expiresAt}
          credential={state.credential}
        />
      )}
    </section>
  );
}

/**
 * The one-time token and the credential, shown where the person can
 * copy them and nowhere else afterwards.
 *
 * Each warning sits above its value rather than below it: someone who
 * has already navigated away has no way back, so it has to be read
 * before the panel is dismissed.
 */
function Registered({
  href,
  name,
  token,
  expiresAt,
  credential,
}: {
  href: string;
  name: string;
  token: string;
  expiresAt: string;
  credential: string;
}) {
  return (
    <div className="flex max-w-2xl flex-col gap-3 rounded-md border border-border p-4">
      <h3 className="font-heading text-sm font-semibold wrap-anywhere">
        Registered {name}
      </h3>
      <h4 className="text-sm font-medium">Credential</h4>
      <p className="text-sm text-destructive">
        This is the only time this credential is shown; garam keeps no copy. If
        it is lost, re-issue it on the operator&apos;s page, which voids this
        one.
      </p>
      <SecretField value={credential} label="Credential" noun="credential" />
      <p className="text-sm text-muted-foreground">
        Place it in the operator&apos;s cluster as a file only the operator
        reads. The operator exchanges it at{" "}
        <code className="font-mono">POST /tokens</code> on garam&apos;s machine
        listener for the short-lived tokens it authenticates with.
      </p>
      <h4 className="text-sm font-medium">Enrollment token</h4>
      <p className="text-sm text-destructive">
        This is the only time this token is shown; garam keeps no copy. If it is
        lost, ask an administrator to mint a new enrollment token.
      </p>
      <SecretField value={token} label="Enrollment token" noun="token" />
      <p className="text-sm text-muted-foreground">
        An operator that still obtains a certificate enrols with it at{" "}
        <code className="font-mono">POST /enrollment</code> on garam&apos;s
        machine listener. It works once and expires at{" "}
        <ExpiresAt at={expiresAt} />.
      </p>
      <div>
        <Button asChild variant="outline">
          <Link href={href}>Open operator</Link>
        </Button>
      </div>
    </div>
  );
}

/** The refusal a parse carried, or null when it carried none. */
function errorOf<T>(parsed: Parsed<T>): string | null {
  return parsed.status === "error" ? parsed.message : null;
}

/**
 * The expiry, rendered in the reader's own time zone.
 *
 * It is a client component and mounted with `suppressHydrationWarning`
 * because the server renders it in a different zone, and the difference
 * is the point rather than a defect.
 */
function ExpiresAt({ at }: { at: string }) {
  return (
    <time dateTime={at} suppressHydrationWarning>
      {new Date(at).toLocaleTimeString()}
    </time>
  );
}
