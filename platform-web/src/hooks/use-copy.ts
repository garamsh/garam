"use client";

import { useState } from "react";

/** What the clipboard answered for one value. */
export type CopyOutcome = "copied" | "failed";

/**
 * Copies a value and remembers what the clipboard answered for it. The
 * outcome is keyed by the value: a different value is a different
 * thing to copy, so neither "copied" nor a failure carries over to it.
 */
export function useCopy(value: string): {
  outcome: CopyOutcome | null;
  copy: () => Promise<CopyOutcome>;
} {
  const [attempt, setAttempt] = useState<{
    value: string;
    outcome: CopyOutcome;
  } | null>(null);

  async function copy(): Promise<CopyOutcome> {
    let outcome: CopyOutcome;
    // No boundary catches a handler. `navigator.clipboard` is absent
    // outside a secure context, so the try covers the property access
    // and not only the rejection.
    try {
      await navigator.clipboard.writeText(value);
      outcome = "copied";
    } catch {
      outcome = "failed";
    }
    setAttempt({ value, outcome });
    return outcome;
  }

  return { outcome: attempt?.value === value ? attempt.outcome : null, copy };
}
