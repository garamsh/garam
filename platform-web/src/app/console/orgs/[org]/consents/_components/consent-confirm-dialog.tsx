"use client";

import { Button } from "@garam/design/ui/button";

import { ActionDialog } from "../../../../_components/action-dialog";

/**
 * What a consent write answers, as far as this dialog reads it: a
 * refusal keeps it open, anything else closes it.
 */
type ConsentWriteState =
  { status: "idle" | "granted" } | { status: "error"; message: string };

type Idle = { status: "idle" };
const IDLE: Idle = { status: "idle" };

/**
 * Asks before a write to this organization's half of one pair, in a
 * dialog whose button names the act and whose text says what it does
 * to messages (ADR-0103 §Patterns). The pair is shown by the other
 * organization's identifier, in mono, as the one thing on screen that
 * says which pair it is. A write that lands closes the dialog and the
 * card re-renders as the pair now stands.
 */
export function ConsentConfirmDialog({
  org,
  other,
  action,
  act,
  doing,
  title,
  consequence,
  destructive = false,
}: {
  org: string;
  other: string;
  /**
   * The Server Action that writes the half. It reads no previous state,
   * so it is always handed an idle one.
   */
  action: (state: Idle, formData: FormData) => Promise<ConsentWriteState>;
  /** The act, as both buttons name it: "Grant consent". */
  act: string;
  /** The submit button's label while the act is in flight. */
  doing: string;
  title: string;
  /** What the act does to messages, in a sentence. */
  consequence: string;
  destructive?: boolean;
}) {
  return (
    <ActionDialog<ConsentWriteState>
      trigger={
        <Button variant={destructive ? "outline" : "default"}>{act}</Button>
      }
      title={title}
      description={<p>{consequence}</p>}
      action={(_state, formData) => action(IDLE, formData)}
      initialState={IDLE}
      fields={{ org, other }}
      confirmLabel={act}
      pendingLabel={doing}
      destructive={destructive}
      done={(state) => state.status !== "error"}
    >
      {() => (
        <p className="flex flex-wrap items-center gap-2 text-muted-foreground">
          Other organization
          <code className="rounded-md bg-muted px-1.5 py-0.5 font-mono text-table text-foreground">
            {other}
          </code>
        </p>
      )}
    </ActionDialog>
  );
}
