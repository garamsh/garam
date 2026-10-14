"use client";

import { Check } from "lucide-react";
import { useActionState, useState } from "react";

import { DescriptionList } from "@/components/description-list";
import { FormField } from "@/components/form-field";
import { Time } from "@/components/time";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  grantDelegation,
  withdrawDelegation,
  type DelegationState,
} from "@/features/orgs/actions";
import type { DelegatedOperation, Delegation } from "@/features/orgs";
import { DELEGATED_OPERATION_LABEL } from "@/lib/delegated-operations";
import { useHydrated } from "@/hooks/use-hydrated";

const initialState: DelegationState = { status: "idle" };

/**
 * The operations the form offers a grant, in the order it lists them,
 * each with what it lets the operator do.
 */
const OPERATIONS: readonly { value: DelegatedOperation; label: string }[] = (
  [
    "agent:create",
    "agent:configure",
    "agent:activate",
    "agent:recover",
    "agent-template:read",
    "agent-template:publish",
    "execution-profile:read",
    "agent:execution-read",
    "operator:read",
  ] as const
).map((value) => ({ value, label: DELEGATED_OPERATION_LABEL[value] }));

/** An operator a grant may name as a controller. */
export type Controller = { grn: string; name: string; identifier: string };

/**
 * The hosted operator's delegation: what it may do and for which
 * controllers, until when (ADR-0078). A grant is never edited: saving
 * ends the one shown and writes another, refused where another write
 * moved it first, and withdrawing ends the one shown and only that.
 * Controllers are named, never shown by GRN (ADR-0011). `canChange` is
 * false only where the server denied delegating, and the grant is then
 * shown alone, with nothing offered that garam would refuse.
 */
export function DelegationForm({
  org,
  operator,
  delegation,
  controllers,
  canChange = true,
}: {
  org: string;
  operator: string;
  delegation: Delegation | null;
  /** The organization's other operators, which a grant may name. */
  controllers: readonly Controller[];
  canChange?: boolean;
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
  // Held in state so a refused grant keeps what was chosen: React
  // resets uncontrolled fields to their defaults after the action.
  const [chosenControllers, setChosenControllers] = useState<string[]>(
    delegation?.controllers ?? [],
  );
  const [chosenOperations, setChosenOperations] = useState<string[]>(
    delegation?.operations ?? [],
  );
  const [days, setDays] = useState("7");
  const toggle = (list: string[], value: string, on: boolean) =>
    on ? [...list, value] : list.filter((item) => item !== value);
  const byGrn = new Map(controllers.map((c) => [c.grn, c]));

  return (
    <div className="flex max-w-2xl flex-col gap-6">
      {delegation === null ? (
        <p className="text-muted-foreground">
          This operator holds no delegation, so the console hands it no
          operation.
        </p>
      ) : (
        <DescriptionList
          items={[
            {
              term: "Operations",
              description: (
                <ul className="flex flex-col gap-0.5">
                  {delegation.operations.map((operation) => (
                    <li key={operation}>
                      {DELEGATED_OPERATION_LABEL[operation]}
                    </li>
                  ))}
                </ul>
              ),
            },
            {
              term: "Controllers",
              description: (
                <ul className="flex flex-col gap-0.5">
                  {delegation.controllers.map((grn) => (
                    <li key={grn}>
                      {byGrn.get(grn)?.name ?? (
                        <span className="text-muted-foreground">
                          An operator no longer registered
                        </span>
                      )}
                    </li>
                  ))}
                </ul>
              ),
            },
            {
              term: "Expires",
              description: <Time value={delegation.expiresAt} />,
            },
          ]}
        />
      )}
      {canChange && (
        <>
          <form action={grantAction} className="flex flex-col gap-5">
            <input type="hidden" name="org" value={org} />
            <input type="hidden" name="operator" value={operator} />
            <input
              type="hidden"
              name="expectedGrantId"
              value={delegation?.grantId ?? ""}
            />
            <fieldset className="flex flex-col gap-2">
              <legend className="mb-1 font-medium">Controllers</legend>
              {controllers.length === 0 ? (
                <p className="text-muted-foreground">
                  Register another operator to name it as a controller.
                </p>
              ) : (
                controllers.map((c) => (
                  <label key={c.grn} className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      name="controllers"
                      value={c.grn}
                      checked={chosenControllers.includes(c.grn)}
                      onChange={(event) =>
                        setChosenControllers((list) =>
                          toggle(list, c.grn, event.target.checked),
                        )
                      }
                      disabled={!hydrated}
                      className="size-4 accent-primary"
                    />
                    {c.name}
                    <span className="font-mono text-xs text-muted-foreground">
                      {c.identifier}
                    </span>
                  </label>
                ))
              )}
            </fieldset>
            <fieldset className="flex flex-col gap-2">
              <legend className="mb-1 font-medium">Operations</legend>
              {OPERATIONS.map((o) => (
                <label key={o.value} className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    name="operations"
                    value={o.value}
                    checked={chosenOperations.includes(o.value)}
                    onChange={(event) =>
                      setChosenOperations((list) =>
                        toggle(list, o.value, event.target.checked),
                      )
                    }
                    disabled={!hydrated}
                    className="size-4 accent-primary"
                  />
                  {o.label}
                </label>
              ))}
            </fieldset>
            <FormField
              label="Days until it expires"
              hint="From 1 to 30."
              error={granted.status === "error" ? granted.message : undefined}
              className="max-w-48"
            >
              {(control) => (
                <Input
                  {...control}
                  name="days"
                  type="number"
                  min={1}
                  max={30}
                  value={days}
                  onChange={(event) => setDays(event.target.value)}
                  disabled={!hydrated}
                />
              )}
            </FormField>
            <div className="flex flex-wrap items-center gap-2">
              <Button
                type="submit"
                disabled={!hydrated || granting || controllers.length === 0}
              >
                {delegation === null
                  ? "Grant delegation"
                  : "Replace delegation"}
              </Button>
              {granted.status === "granted" && (
                <span
                  role="status"
                  className="flex items-center gap-1 text-muted-foreground"
                >
                  <Check aria-hidden className="size-4 text-ok" />
                  Delegation granted
                </span>
              )}
            </div>
          </form>
          {delegation !== null && (
            <div className="flex flex-wrap items-center gap-2 border-t pt-4">
              <AlertDialog>
                <AlertDialogTrigger asChild>
                  <Button variant="outline" disabled={!hydrated || withdrawing}>
                    Withdraw delegation
                  </Button>
                </AlertDialogTrigger>
                <AlertDialogContent>
                  <form action={withdrawAction} className="contents">
                    <input type="hidden" name="org" value={org} />
                    <input type="hidden" name="operator" value={operator} />
                    <input
                      type="hidden"
                      name="grantId"
                      value={delegation.grantId}
                    />
                    <AlertDialogHeader>
                      <AlertDialogTitle>
                        Withdraw this delegation?
                      </AlertDialogTitle>
                      <AlertDialogDescription>
                        The grant ends when you confirm, and the console hands
                        this operator no further operations under it.
                      </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                      <AlertDialogCancel>Cancel</AlertDialogCancel>
                      <AlertDialogAction type="submit" variant="destructive">
                        Withdraw delegation
                      </AlertDialogAction>
                    </AlertDialogFooter>
                  </form>
                </AlertDialogContent>
              </AlertDialog>
              {withdrawn.status === "error" && (
                <p role="alert" className="text-destructive">
                  {withdrawn.message}
                </p>
              )}
              {withdrawn.status === "withdrawn" && (
                <span
                  role="status"
                  className="flex items-center gap-1 text-muted-foreground"
                >
                  <Check aria-hidden className="size-4 text-ok" />
                  Delegation withdrawn
                </span>
              )}
            </div>
          )}
        </>
      )}
    </div>
  );
}
