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

import { orgHref } from "../../../../../_components/navigation";

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
 * The enrollment token comes back with the record and is rendered
 * once. Nothing can fetch it again — garam stores its hash — so the
 * panel says so where the person reads the token rather than
 * afterwards (ADR-0064).
 *
 * Only what has to be read before typing stands above the field: what
 * registering does. What the operator does with the token is beside
 * the token, and what deleting a record does is on the operator's own
 * page, beside the control that does it.
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
        back a one-time token it enrols with.
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
          <Button type="submit" disabled={pending || problem !== null}>
            {pending ? "Registering…" : "Register operator"}
          </Button>
        </div>
      </form>

      {state.status === "registered" && (
        <EnrollmentToken
          href={`${orgHref(org, "operators")}/${state.operator}`}
          name={state.name}
          token={state.token}
          expiresAt={state.expiresAt}
        />
      )}
    </section>
  );
}

/**
 * The one-time token, shown where the person can copy it and nowhere
 * else afterwards.
 *
 * The warning sits above the token rather than below it: someone who
 * has already navigated away has no way back, so it has to be read
 * before the panel is dismissed.
 */
function EnrollmentToken({
  href,
  name,
  token,
  expiresAt,
}: {
  href: string;
  name: string;
  token: string;
  expiresAt: string;
}) {
  // Keyed by token, as the invitation panel is: a later registration is
  // a different token, so neither the claim that it was copied nor the
  // failure the previous one reported carries over to it.
  const [attempt, setAttempt] = useState<CopyAttempt | null>(null);
  const outcome = attempt?.token === token ? attempt.outcome : null;

  return (
    <div className="flex max-w-2xl flex-col gap-3 rounded-md border border-border p-4">
      <h3 className="font-heading text-sm font-semibold wrap-anywhere">
        Registered {name}
      </h3>
      <p className="text-sm text-destructive">
        This is the only time this token is shown. garam keeps no copy and no
        page can retrieve it. If you lose it, delete this record and register
        the operator again, or have an administrator issue a certificate out of
        band.
      </p>
      <div className="flex gap-2">
        <Input
          readOnly
          value={token}
          aria-label="Enrollment token"
          onFocus={(event) => event.target.select()}
          className="min-w-0 flex-1 bg-muted font-mono text-xs"
        />
        <Button
          type="button"
          variant="outline"
          onClick={async () => {
            // No boundary catches a handler. `navigator.clipboard` is
            // absent outside a secure context, so the try covers the
            // property access and not only the rejection.
            try {
              await navigator.clipboard.writeText(token);
              setAttempt({ token, outcome: "copied" });
            } catch {
              setAttempt({ token, outcome: "failed" });
            }
          }}
        >
          {outcome === "copied" ? "Copied" : "Copy"}
        </Button>
      </div>
      {outcome === "failed" && (
        <p role="alert" className="text-sm text-destructive">
          Could not copy the token. Select it in the field and copy it yourself.
        </p>
      )}
      <p className="text-sm text-muted-foreground">
        The operator generates its own key pair in its own cluster and presents
        the token to get a certificate signed — no private key is ever shown
        here or held by garam.
      </p>
      <p className="text-sm text-muted-foreground">
        Give it to the operator, which presents it with a certificate signing
        request to <code className="font-mono">POST /enrollment</code> on
        garam&apos;s machine listener. It is spent by the first attempt and
        stops working at <ExpiresAt at={expiresAt} />.
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

/** What the clipboard answered for one enrollment token. */
type CopyAttempt = { token: string; outcome: "copied" | "failed" };

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
