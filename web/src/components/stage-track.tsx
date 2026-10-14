import { Check, X } from "lucide-react";

import { cn } from "@/lib/utils";

/** One stage of a lifecycle, in the order the lifecycle passes them. */
export type Stage = { id: string; label: string };

type StageState = "done" | "current" | "failed" | "upcoming";

const STATE_TEXT: Record<StageState, string> = {
  done: "completed",
  current: "current stage",
  failed: "failed",
  upcoming: "not reached",
};

/**
 * Where an object stands in a lifecycle such as a cutover or a
 * recovery: the stages behind it, the one it is in, and the ones ahead.
 * `failed` marks the current stage as where it stopped. Each stage says
 * its state in words, so the track reads without its colours.
 */
export function StageTrack({
  label,
  stages,
  current,
  failed = false,
  className,
}: {
  /** What lifecycle this is, as the list's accessible name. */
  label: string;
  stages: readonly Stage[];
  current: string;
  failed?: boolean;
  className?: string;
}) {
  const at = stages.findIndex((stage) => stage.id === current);
  return (
    <ol
      aria-label={label}
      className={cn("flex flex-wrap items-center gap-x-2 gap-y-3", className)}
    >
      {stages.map((stage, index) => {
        const state: StageState =
          index < at
            ? "done"
            : index > at
              ? "upcoming"
              : failed
                ? "failed"
                : "current";
        return (
          <li
            key={stage.id}
            data-state={state}
            aria-current={index === at ? "step" : undefined}
            className="flex items-center gap-2"
          >
            {index > 0 && (
              <span
                aria-hidden
                className={cn(
                  "h-px w-6 sm:w-10",
                  index <= at ? "bg-primary" : "bg-border",
                )}
              />
            )}
            <span
              aria-hidden
              className={cn(
                "flex size-5 shrink-0 items-center justify-center rounded-full border text-[0.625rem]",
                state === "done" &&
                  "border-primary bg-primary text-primary-foreground",
                state === "current" && "border-primary ring-3 ring-primary/20",
                state === "failed" && "border-danger bg-danger text-white",
                state === "upcoming" && "border-border",
              )}
            >
              {state === "done" && (
                <Check className="size-3" strokeWidth={2.5} />
              )}
              {state === "failed" && <X className="size-3" strokeWidth={2.5} />}
              {state === "current" && (
                <span className="size-1.5 rounded-full bg-primary" />
              )}
            </span>
            <span
              className={cn(
                "text-sm",
                state === "upcoming" ? "text-muted-foreground" : "font-medium",
              )}
            >
              {stage.label}
              <span className="sr-only">, {STATE_TEXT[state]}</span>
            </span>
          </li>
        );
      })}
    </ol>
  );
}
