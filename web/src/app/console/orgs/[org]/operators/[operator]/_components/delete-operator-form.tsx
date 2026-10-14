"use client";

import Link from "next/link";
import { useActionState, useId, useState } from "react";

import {
  AlertDialog,
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
import { Label } from "@/components/ui/label";
import {
  deleteOperator,
  type DeleteOperatorState,
} from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";

const initialState: DeleteOperatorState = { status: "idle" };

/** A definition that names the operator, which refuses its deletion. */
export type BlockingDefinition = { name: string; href: string };

/**
 * Deletes one operator's registration, which stops that operator
 * authenticating to garam (ADR-0070). It ends a machine identity, so it
 * is slow on purpose: the person types the identifier, and the dialog
 * says what stops and what does not — nothing here reaches into the
 * cluster. garam refuses the deletion while a definition names the
 * operator, so those definitions are named here and the control waits
 * for them to go. `blocking` is null when the definitions were not
 * listed to the caller, and the server then decides.
 */
export function DeleteOperatorForm({
  org,
  operator,
  name,
  blocking,
}: {
  org: string;
  operator: string;
  name: string;
  blocking: readonly BlockingDefinition[] | null;
}) {
  const [state, formAction, pending] = useActionState(
    deleteOperator,
    initialState,
  );
  const hydrated = useHydrated();
  const [typed, setTyped] = useState("");
  const confirmId = useId();
  const blocked = blocking !== null && blocking.length > 0;

  return (
    <AlertDialog onOpenChange={() => setTyped("")}>
      <AlertDialogTrigger asChild>
        <Button variant="destructive" disabled={!hydrated}>
          Delete operator
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent className="data-[size=default]:sm:max-w-md">
        <form action={formAction} className="contents">
          <input type="hidden" name="org" value={org} />
          <input type="hidden" name="operator" value={operator} />
          <AlertDialogHeader>
            <AlertDialogTitle>Delete {name}?</AlertDialogTitle>
            <AlertDialogDescription>
              garam refuses its keys, its credential and its certificate from
              the next request. Work already running in its cluster does not
              stop, and agents it provisioned keep their own credentials.
            </AlertDialogDescription>
          </AlertDialogHeader>
          {blocked ? (
            <div className="flex flex-col gap-2">
              <p>
                It can&apos;t be deleted while{" "}
                {blocking.length === 1
                  ? "this agent definition names it"
                  : `these ${blocking.length} agent definitions name it`}
                :
              </p>
              <ul className="flex flex-col gap-1">
                {blocking.map((definition) => (
                  <li key={definition.href}>
                    <Link
                      href={definition.href}
                      className="font-medium underline-offset-4 hover:underline"
                    >
                      {definition.name}
                    </Link>
                  </li>
                ))}
              </ul>
            </div>
          ) : (
            <div className="flex flex-col gap-2">
              <Label htmlFor={confirmId}>
                Type <span className="font-mono">{operator}</span> to confirm
              </Label>
              <Input
                id={confirmId}
                value={typed}
                onChange={(event) => setTyped(event.target.value)}
                autoComplete="off"
                spellCheck={false}
                className="font-mono"
              />
            </div>
          )}
          {state.status === "error" && (
            <p role="alert" className="text-destructive">
              {state.message}
            </p>
          )}
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <Button
              type="submit"
              variant="destructive"
              disabled={blocked || typed !== operator || pending}
            >
              {pending ? "Deleting…" : "Delete operator"}
            </Button>
          </AlertDialogFooter>
        </form>
      </AlertDialogContent>
    </AlertDialog>
  );
}
