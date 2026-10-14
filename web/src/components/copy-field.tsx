"use client";

import { Check, Copy } from "lucide-react";
import { useId } from "react";

import { Button } from "@/components/ui/button";
import { useCopy } from "@/hooks/use-copy";
import { cn } from "@/lib/utils";

/**
 * An identifier shown in mono with a control that copies it: an
 * object's id, an endpoint, a key fingerprint. Not for a secret —
 * a value shown once is `SecretReveal`'s.
 */
export function CopyField({
  label,
  value,
  hideLabel = false,
  className,
}: {
  /** What the value is; it names the copy control too. */
  label: string;
  value: string;
  /** Keep the label for assistive technology only, as in a header. */
  hideLabel?: boolean;
  className?: string;
}) {
  const id = useId();
  const { outcome, copy } = useCopy(value);

  return (
    // The group carries the label: `code` takes no name of its own
    // (ARIA prohibits naming that role).
    <div
      role="group"
      aria-labelledby={id}
      className={cn("flex min-w-0 flex-col gap-1", className)}
    >
      <span
        id={id}
        className={cn("text-muted-foreground", hideLabel && "sr-only")}
      >
        {label}
      </span>
      <div className="flex min-w-0 items-center gap-1">
        <code className="min-w-0 truncate rounded-md bg-muted px-1.5 py-0.5 font-mono text-table">
          {value}
        </code>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={`Copy ${label.toLowerCase()}`}
          onClick={copy}
        >
          {outcome === "copied" ? <Check className="text-ok" /> : <Copy />}
        </Button>
        <span aria-live="polite" className="text-xs text-muted-foreground">
          {outcome === "copied"
            ? "Copied"
            : outcome === "failed"
              ? "Copy failed — select the text instead"
              : ""}
        </span>
      </div>
    </div>
  );
}
