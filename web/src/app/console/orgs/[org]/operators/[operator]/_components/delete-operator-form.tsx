"use client";

import Link from "next/link";

import { Button } from "@/components/ui/button";
import {
  deleteOperator,
  type DeleteOperatorState,
} from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";

import { ActionDialog } from "../../../../../_components/action-dialog";

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
  const hydrated = useHydrated();
  const blocked = blocking !== null && blocking.length > 0;

  return (
    <ActionDialog
      trigger={
        <Button variant="destructive" disabled={!hydrated}>
          Delete operator
        </Button>
      }
      title={`Delete ${name}?`}
      description={
        <p>
          garam refuses its keys, its credential and its certificate from the
          next request. Work already running in its cluster does not stop, and
          agents it provisioned keep their own credentials.
        </p>
      }
      action={deleteOperator}
      initialState={initialState}
      fields={{ org, operator }}
      command={{
        label: "Delete operator",
        consequence:
          "garam refuses its keys and credential; work in its cluster does not stop.",
      }}
      confirmLabel="Delete operator"
      pendingLabel="Deleting…"
      destructive
      typed={{ phrase: operator, mono: true }}
      blocked={blocked}
      done={(state) => state.status !== "error"}
    >
      {() =>
        blocked && (
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
        )
      }
    </ActionDialog>
  );
}
