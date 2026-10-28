"use client";

import { Check, Copy } from "lucide-react";
import { useEffect, useState } from "react";

import { cn } from "@garam/design/lib/utils";

type CopyState = "idle" | "copied" | "failed";

/**
 * An identifier as the console offers it: monospaced, beside the name it
 * belongs to and never in that name's place, and copied with one click.
 * It is the only way an identifier reaches a person, so none is ever
 * read as a name, a title or a sentence.
 *
 * `label` names what the identifier is for — "Alice", "the Backend
 * project" — so a screen reader hears which one it copies. The button
 * is at least 24px each way, WCAG 2.2's minimum target, so a caller
 * sets no gap to make it one.
 */
export function CopyId({
  value,
  label,
  className,
}: {
  value: string;
  label: string;
  className?: string;
}) {
  const [state, setState] = useState<CopyState>("idle");

  useEffect(() => {
    if (state === "idle") return;
    const reset = setTimeout(() => setState("idle"), 1500);
    return () => clearTimeout(reset);
  }, [state]);

  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setState("copied");
    } catch {
      // A browser outside a secure context, or one that refused the
      // permission, has no clipboard to write to.
      setState("failed");
    }
  }

  const Icon = state === "copied" ? Check : Copy;
  return (
    <span className={cn("inline-flex max-w-full", className)}>
      <button
        type="button"
        onClick={copy}
        title="Copy identifier"
        aria-label={`Copy the identifier of ${label}: ${value}`}
        className="inline-flex min-h-6 min-w-6 max-w-full items-center gap-1 rounded-sm font-mono text-xs font-normal text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
      >
        <span className="truncate">{value}</span>
        <Icon aria-hidden className="size-3 shrink-0" strokeWidth={1.5} />
      </button>
      <span aria-live="polite" className="sr-only">
        {state === "copied"
          ? "Copied"
          : state === "failed"
            ? "Couldn't copy: this browser blocked the clipboard."
            : ""}
      </span>
    </span>
  );
}
