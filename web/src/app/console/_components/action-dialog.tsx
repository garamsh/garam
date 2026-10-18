"use client";

import { Check } from "lucide-react";
import { useState, useTransition } from "react";

import { Refusal } from "@/components/refusal";
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

import { useReportOutcome, type OutcomeArea } from "./outcome";
import { TypedConfirm } from "./typed-confirm";

/** What every Server Action a dialog sends answers with. */
type ActionState = { status: string; message?: string };

type FormProps<S extends ActionState> = {
  title: string;
  description: React.ReactNode;
  /** What the form carries beyond its fields: a diff, a confirmation. */
  children?: (state: S) => React.ReactNode;
  action: (state: S, formData: FormData) => Promise<S>;
  initialState: S;
  /** The hidden values the write is sent with. */
  fields: Record<string, string | number>;
  confirmLabel: string | ((state: S) => string);
  pendingLabel: string;
  destructive?: boolean;
  /**
   * What the person types before an act nothing undoes can be
   * confirmed; `mono` where it is an identifier.
   */
  typed?: { phrase: string; mono?: boolean };
  /** The act cannot be confirmed now; the dialog's text says why. */
  blocked?: boolean;
  /** Whether an answer means the write landed. */
  done: (state: S) => boolean;
};

/**
 * The console's one confirmation of a write (ADR-0103 §Patterns): a
 * dialog naming the act on its button ("Revoke key", never "Confirm"),
 * which sends the write from its own form and stays open until garam
 * answers. A write that lands closes it; a refusal stays in it, and goes
 * when it closes. Where the caller says what the write did, `outcome`
 * puts it beside the control that opened the dialog.
 */
export function ActionDialog<S extends ActionState>({
  trigger,
  outcome,
  area,
  onAnswer,
  onOpen,
  ...form
}: FormProps<S> & {
  /** The control opening the dialog; a `Button`. */
  trigger: React.ReactElement;
  /** What a landed write reports; null where it reports nothing. */
  outcome?: (state: S) => string | null;
  /**
   * Where the page says `outcome`, under an `OutcomeProvider`. Without
   * one, or without an area, it is said beside the control.
   */
  area?: OutcomeArea;
  /** Every answer, for a caller that shows some of them elsewhere. */
  onAnswer?: (state: S) => void;
  /** Each time the dialog opens, for a caller that reads again then. */
  onOpen?: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [said, setSaid] = useState<string | null>(null);
  const report = useReportOutcome();
  const answered = (next: S) => {
    onAnswer?.(next);
    const landed = form.done(next);
    if (landed) setOpen(false);
    const text = landed ? (outcome?.(next) ?? null) : null;
    if (area !== undefined && report !== null) {
      if (text !== null) report(area, text);
    } else {
      setSaid(text);
    }
  };

  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
      <AlertDialog
        open={open}
        onOpenChange={(next) => {
          setOpen(next);
          if (next) onOpen?.();
        }}
      >
        <AlertDialogTrigger asChild>{trigger}</AlertDialogTrigger>
        <AlertDialogContent className="sm:max-w-lg">
          <ConfirmForm {...form} onAnswer={answered} />
        </AlertDialogContent>
      </AlertDialog>
      {said !== null && (
        <span
          role="status"
          className="flex items-center gap-1.5 text-muted-foreground"
        >
          <Check aria-hidden className="size-4 shrink-0 text-ok" />
          {said}
        </span>
      )}
    </div>
  );
}

/**
 * The dialog's form. It is mounted only while the dialog is open, so
 * what it holds — a refusal, a typed phrase — goes when it closes.
 */
function ConfirmForm<S extends ActionState>({
  title,
  description,
  children,
  action,
  initialState,
  fields,
  confirmLabel,
  pendingLabel,
  destructive = false,
  typed,
  blocked = false,
  done,
  onAnswer,
}: FormProps<S> & { onAnswer: (state: S) => void }) {
  const [state, setState] = useState<S>(initialState);
  const [matched, setMatched] = useState(typed === undefined);
  const [pending, startTransition] = useTransition();
  const formAction = (formData: FormData) =>
    startTransition(async () => {
      const next = await action(state, formData);
      // State set after an await is its own transition (React 19).
      startTransition(() => {
        setState(next);
        onAnswer(next);
      });
    });
  const label =
    typeof confirmLabel === "function" ? confirmLabel(state) : confirmLabel;
  const refused =
    !done(state) && state.status === "error" && state.message !== undefined;

  return (
    <form action={formAction} className="flex min-w-0 flex-col gap-4">
      {Object.entries(fields).map(([name, value]) => (
        <input key={name} type="hidden" name={name} value={value} />
      ))}
      <AlertDialogHeader>
        <AlertDialogTitle>{title}</AlertDialogTitle>
        <AlertDialogDescription asChild>
          <div className="flex flex-col gap-2">{description}</div>
        </AlertDialogDescription>
      </AlertDialogHeader>
      {children?.(state)}
      {typed !== undefined && !blocked && (
        <TypedConfirm {...typed} onMatch={setMatched} />
      )}
      {refused && <Refusal>{state.message}</Refusal>}
      <AlertDialogFooter>
        <AlertDialogCancel disabled={pending}>Cancel</AlertDialogCancel>
        <Button
          type="submit"
          variant={destructive ? "destructive" : "default"}
          disabled={blocked || !matched || pending}
        >
          {pending ? pendingLabel : label}
        </Button>
      </AlertDialogFooter>
    </form>
  );
}
