"use client";

import { useActionState, useState } from "react";

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
 * says which pair it is.
 *
 * The dialog stays open until the action answers, so a refusal is read
 * in it; a write that lands closes it and the card re-renders as the
 * pair now stands.
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
  const [open, setOpen] = useState(false);
  const [state, formAction, pending] = useActionState(
    async (_previous: ConsentWriteState, formData: FormData) => {
      const next = await action(IDLE, formData);
      if (next.status !== "error") setOpen(false);
      return next;
    },
    IDLE,
  );

  return (
    <AlertDialog open={open} onOpenChange={setOpen}>
      <div>
        <AlertDialogTrigger asChild>
          <Button variant={destructive ? "outline" : "default"}>{act}</Button>
        </AlertDialogTrigger>
      </div>
      <AlertDialogContent>
        <form action={formAction} className="grid gap-4">
          <AlertDialogHeader>
            <AlertDialogTitle>{title}</AlertDialogTitle>
            <AlertDialogDescription>{consequence}</AlertDialogDescription>
          </AlertDialogHeader>
          <p className="flex flex-wrap items-center gap-2 text-muted-foreground">
            Other organization
            <code className="rounded-md bg-muted px-1.5 py-0.5 font-mono text-table text-foreground">
              {other}
            </code>
          </p>
          <input type="hidden" name="org" value={org} />
          <input type="hidden" name="other" value={other} />
          {state.status === "error" && (
            <p role="alert" className="text-danger">
              {state.message}
            </p>
          )}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={pending}>Cancel</AlertDialogCancel>
            <Button
              type="submit"
              variant={destructive ? "destructive" : "default"}
              disabled={pending}
            >
              {pending ? doing : act}
            </Button>
          </AlertDialogFooter>
        </form>
      </AlertDialogContent>
    </AlertDialog>
  );
}
