"use client";

import { useState, useTransition } from "react";

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

/** What every Server Action a dialog sends answers with. */
type ActionState = { status: string; message?: string };

/**
 * A write confirmed in a dialog that names the act on its button
 * ("Archive policy", never "Confirm"). The form is the dialog's, so the
 * write is sent from it and its answer read there: the dialog closes
 * once `done` says the write landed, and otherwise stays open with the
 * refusal under the form. Where the caller says what the write did,
 * `outcome` puts it beside the control that opened the dialog.
 */
export function ActionDialog<S extends ActionState>({
  trigger,
  title,
  description,
  children,
  action,
  initialState,
  fields,
  confirmLabel,
  pendingLabel,
  destructive = false,
  done,
  outcome,
  area,
  onAnswer,
}: {
  /** The control opening the dialog; a `Button`. */
  trigger: React.ReactElement;
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
  /** Whether an answer means the write landed. */
  done: (state: S) => boolean;
  /** What a landed write reports; null where it reports nothing. */
  outcome?: (state: S) => string | null;
  /**
   * Where the page says `outcome`, under an `OutcomeProvider`. Without
   * one, or without an area, it is said beside the control.
   */
  area?: OutcomeArea;
  /** Every answer, for a caller that shows some of them elsewhere. */
  onAnswer?: (state: S) => void;
}) {
  const [open, setOpen] = useState(false);
  const [state, setState] = useState<S>(initialState);
  // Whether the last answer was a write that landed: several actions
  // answer success with the state they start in.
  const [landed, setLanded] = useState(false);
  const [pending, startTransition] = useTransition();
  const report = useReportOutcome();
  const elsewhere = area !== undefined && report !== null;
  const formAction = (formData: FormData) =>
    startTransition(async () => {
      const next = await action(state, formData);
      // State set after an await is its own transition (React 19).
      startTransition(() => {
        setState(next);
        onAnswer?.(next);
        setLanded(done(next));
        if (!done(next)) return;
        setOpen(false);
        const said = outcome?.(next) ?? null;
        if (elsewhere && said !== null) report(area, said);
      });
    });
  const label =
    typeof confirmLabel === "function" ? confirmLabel(state) : confirmLabel;
  const reported = elsewhere || !landed ? null : (outcome?.(state) ?? null);
  const refused =
    !done(state) && state.status === "error" && state.message !== undefined;

  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
      <AlertDialog open={open} onOpenChange={setOpen}>
        <AlertDialogTrigger asChild>{trigger}</AlertDialogTrigger>
        <AlertDialogContent className="sm:max-w-lg">
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
            {refused && (
              <p role="alert" className="text-sm text-destructive">
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
                {pending ? pendingLabel : label}
              </Button>
            </AlertDialogFooter>
          </form>
        </AlertDialogContent>
      </AlertDialog>
      <span
        role="status"
        className="text-sm text-muted-foreground empty:hidden"
      >
        {reported}
      </span>
    </div>
  );
}
