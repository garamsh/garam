"use client";

import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@garam/design/ui/tooltip";
import { formatTime } from "@/lib/format-time";
import { cn } from "@garam/design/lib/utils";

/**
 * An instant from the API: relative in the text, exact with its zone in
 * a tooltip that focus opens as well as hover. The server and the
 * browser each phrase it against their own clock, and a phrase that
 * crossed a boundary in between is the one difference hydration is
 * told to accept. Mount it under a `TooltipProvider`.
 */
export function Time({
  value,
  className,
}: {
  value: string;
  className?: string;
}) {
  const time = formatTime(value);
  if (!time) {
    return (
      <span className={cn("text-muted-foreground", className)}>
        Unknown time
      </span>
    );
  }
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <time
          dateTime={time.iso}
          tabIndex={0}
          suppressHydrationWarning
          className={cn(
            "rounded-sm tabular-nums underline decoration-muted-foreground/40 decoration-dotted underline-offset-4 outline-none focus-visible:ring-3 focus-visible:ring-ring/50",
            className,
          )}
        >
          {time.relative}
        </time>
      </TooltipTrigger>
      <TooltipContent>
        <span suppressHydrationWarning>{time.absolute}</span>
      </TooltipContent>
    </Tooltip>
  );
}
