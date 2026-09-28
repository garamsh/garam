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
import { EnrollmentToken, ExpiresAt } from "./enrollment-token";

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
          <Button
            type="submit"
            disabled={!hydrated || pending || problem !== null}
          >
            {pending ? "Registering…" : "Register operator"}
          </Button>
        </div>
      </form>

      {state.status === "registered" && (
        <EnrollmentToken title={`Registered ${state.name}`} token={state.token}>
          <p className="text-sm text-muted-foreground">
            Expires at <ExpiresAt at={state.expiresAt} />.
          </p>
          <p className="text-sm">
            Give it to the operator to enrol with. If it is lost, issue a
            replacement from the operator&apos;s page.
          </p>
          <div>
            <Button asChild variant="outline">
              <Link href={`${orgHref(org, "operators")}/${state.operator}`}>
                Open operator
              </Link>
            </Button>
          </div>
        </EnrollmentToken>
      )}
    </section>
  );
}

/** The refusal a parse carried, or null when it carried none. */
function errorOf<T>(parsed: Parsed<T>): string | null {
  return parsed.status === "error" ? parsed.message : null;
}
