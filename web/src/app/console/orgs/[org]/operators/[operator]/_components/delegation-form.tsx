"use client";

import { useActionState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  grantDelegation,
  withdrawDelegation,
  type DelegationState,
} from "@/features/orgs/actions";
import type { DelegatedOperation, Delegation } from "@/features/orgs";
import { useHydrated } from "@/hooks/use-hydrated";

const initialState: DelegationState = { status: "idle" };

/** Every operation a grant can carry, and what it lets the operator do. */
const OPERATIONS: readonly { value: DelegatedOperation; label: string }[] = [
  { value: "agent:create", label: "Create agents from published templates" },
  { value: "agent:configure", label: "Configure an agent's execution" },
  { value: "agent:activate", label: "Activate an agent's runtime generation" },
  { value: "agent:recover", label: "Recover an agent's credential" },
  { value: "agent-template:read", label: "Read published templates" },
  { value: "agent-template:publish", label: "Publish template versions" },
  { value: "execution-profile:read", label: "Read execution profiles" },
  { value: "agent:execution-read", label: "Read an agent's execution" },
  { value: "operator:read", label: "Read operator status" },
];

/**
 * The hosted operator's delegation: what it may do and for which
 * controllers, until when (ADR-0078). A grant is never edited: saving
 * ends the one shown and writes another, refused where another write
 * moved it first, and withdrawing ends the one shown and only that.
 */
export function DelegationForm({
  org,
  operator,
  delegation,
  controllers,
}: {
  org: string;
  operator: string;
  delegation: Delegation | null;
  /** The organization's other operators, which a grant may name. */
  controllers: readonly { grn: string; name: string; identifier: string }[];
}) {
  const [granted, grantAction, granting] = useActionState(
    grantDelegation,
    initialState,
  );
  const [withdrawn, withdrawAction, withdrawing] = useActionState(
    withdrawDelegation,
    initialState,
  );
  const hydrated = useHydrated();
  const error =
    granted.status === "error"
      ? granted.message
      : withdrawn.status === "error"
        ? withdrawn.message
        : null;

  return (
    <div className="flex max-w-2xl flex-col gap-4">
      {delegation === null ? (
        <p className="text-sm">This operator holds no delegation.</p>
      ) : (
        <dl className="grid grid-cols-1 gap-x-4 gap-y-1.5 text-sm sm:grid-cols-[auto_1fr]">
          <dt className="text-muted-foreground">Operations</dt>
          <dd>{delegation.operations.join(", ")}</dd>
          <dt className="text-muted-foreground">Controllers</dt>
          <dd className="font-mono break-all">
            {delegation.controllers.join(", ")}
          </dd>
          <dt className="text-muted-foreground">Expires</dt>
          <dd>
            <time dateTime={delegation.expiresAt}>
              {delegation.expiresAt.slice(0, 16).replace("T", " ")} UTC
            </time>
          </dd>
        </dl>
      )}
      <form action={grantAction} className="flex flex-col gap-3">
        <input type="hidden" name="org" value={org} />
        <input type="hidden" name="operator" value={operator} />
        <input
          type="hidden"
          name="expectedGrantId"
          value={delegation?.grantId ?? ""}
        />
        <fieldset className="flex flex-col gap-1.5">
          <legend className="mb-1 text-sm font-medium">Controllers</legend>
          {controllers.length === 0 && (
            <p className="text-sm text-muted-foreground">
              Register another operator to delegate a controller.
            </p>
          )}
          {controllers.map((c) => (
            <label key={c.grn} className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                name="controllers"
                value={c.grn}
                defaultChecked={delegation?.controllers.includes(c.grn)}
                disabled={!hydrated}
              />
              {c.name} <span className="font-mono">({c.identifier})</span>
            </label>
          ))}
        </fieldset>
        <fieldset className="flex flex-col gap-1.5">
          <legend className="mb-1 text-sm font-medium">Operations</legend>
          {OPERATIONS.map((o) => (
            <label key={o.value} className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                name="operations"
                value={o.value}
                defaultChecked={delegation?.operations.includes(o.value)}
                disabled={!hydrated}
              />
              {o.label} <span className="font-mono">({o.value})</span>
            </label>
          ))}
        </fieldset>
        <div className="flex flex-col gap-2">
          <Label htmlFor="delegation-days">Days until it expires</Label>
          <Input
            id="delegation-days"
            name="days"
            type="number"
            min={1}
            max={30}
            defaultValue={7}
            disabled={!hydrated}
            className="max-w-24"
          />
        </div>
        <div>
          <Button
            type="submit"
            variant="outline"
            disabled={!hydrated || granting || controllers.length === 0}
          >
            {delegation === null ? "Grant delegation" : "Replace delegation"}
          </Button>
        </div>
      </form>
      {delegation !== null && (
        <form action={withdrawAction}>
          <input type="hidden" name="org" value={org} />
          <input type="hidden" name="operator" value={operator} />
          <input type="hidden" name="grantId" value={delegation.grantId} />
          <Button
            type="submit"
            variant="outline"
            disabled={!hydrated || withdrawing}
          >
            Withdraw delegation
          </Button>
        </form>
      )}
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      {granted.status === "granted" && error === null && (
        <p role="status" className="text-sm text-muted-foreground">
          Delegation granted.
        </p>
      )}
      {withdrawn.status === "withdrawn" && error === null && (
        <p role="status" className="text-sm text-muted-foreground">
          Delegation withdrawn.
        </p>
      )}
    </div>
  );
}
