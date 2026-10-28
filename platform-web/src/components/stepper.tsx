import { Check } from "lucide-react";

import { cn } from "@garam/design/lib/utils";

/** One step of a wizard. */
export type Step = { id: string; title: string; description?: string };

/**
 * A wizard's steps, top to bottom: the ones done, the one in hand, and
 * those ahead. `completed` names the steps already finished, so a
 * resumed wizard shows its progress rather than restarting the count.
 */
export function Stepper({
  label,
  steps,
  current,
  completed = [],
  className,
}: {
  label: string;
  steps: readonly Step[];
  current: string;
  completed?: readonly string[];
  className?: string;
}) {
  return (
    <ol aria-label={label} className={cn("flex flex-col", className)}>
      {steps.map((step, index) => {
        const done = completed.includes(step.id);
        const isCurrent = step.id === current;
        return (
          <li
            key={step.id}
            aria-current={isCurrent ? "step" : undefined}
            data-state={done ? "done" : isCurrent ? "current" : "upcoming"}
            className="group relative flex gap-3 pb-6 last:pb-0"
          >
            {index < steps.length - 1 && (
              <span
                aria-hidden
                className={cn(
                  "absolute top-7 bottom-1 left-3 w-px -translate-x-1/2",
                  done ? "bg-primary" : "bg-border",
                )}
              />
            )}
            <span
              aria-hidden
              className={cn(
                "flex size-6 shrink-0 items-center justify-center rounded-full border text-xs font-medium tabular-nums",
                done && "border-primary bg-primary text-primary-foreground",
                !done &&
                  isCurrent &&
                  "border-primary text-primary ring-3 ring-primary/20",
                !done && !isCurrent && "text-muted-foreground",
              )}
            >
              {done ? <Check className="size-3.5 stroke-[2.5]" /> : index + 1}
            </span>
            <div className="flex min-w-0 flex-col gap-0.5 pt-0.5">
              <span
                className={cn(
                  "font-medium",
                  !done && !isCurrent && "text-muted-foreground",
                )}
              >
                {step.title}
                {done && <span className="sr-only"> (done)</span>}
              </span>
              {step.description && (
                <span className="text-muted-foreground">
                  {step.description}
                </span>
              )}
            </div>
          </li>
        );
      })}
    </ol>
  );
}
