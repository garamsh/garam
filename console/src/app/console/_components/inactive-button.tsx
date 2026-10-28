"use client";

import { useId } from "react";

import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";

/**
 * An action the caller cannot take here, kept on the page rather than
 * hidden (ADR-0113): inactive, not disabled, so it stays focusable,
 * and its reason is its description and shows on hover or focus.
 */
export function InactiveButton({
  children,
  reason,
  variant = "outline",
}: {
  children: React.ReactNode;
  reason: string;
  variant?: "default" | "outline";
}) {
  const reasonId = useId();
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant={variant}
          aria-disabled="true"
          aria-describedby={reasonId}
          onClick={(event) => event.preventDefault()}
          className="cursor-not-allowed opacity-50"
        >
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent className="max-w-xs">{reason}</TooltipContent>
      <span id={reasonId} className="sr-only">
        {reason}
      </span>
    </Tooltip>
  );
}
