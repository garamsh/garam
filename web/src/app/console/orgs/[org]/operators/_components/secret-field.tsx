"use client";

import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

/** What the clipboard answered for one value. */
type CopyAttempt = { value: string; outcome: "copied" | "failed" };

/**
 * A secret garam answers once — an enrollment token, an operator
 * credential — in a read-only field with a control that copies it.
 *
 * The copy outcome is keyed by the value: a later answer is a different
 * secret, so neither the claim that it was copied nor the failure the
 * previous one reported carries over to it.
 */
export function SecretField({
  value,
  label,
  noun,
}: {
  value: string;
  /** The field's accessible name. */
  label: string;
  /** What the value is, as the copy failure names it. */
  noun: string;
}) {
  const [attempt, setAttempt] = useState<CopyAttempt | null>(null);
  const outcome = attempt?.value === value ? attempt.outcome : null;

  return (
    <>
      <div className="flex gap-2">
        <Input
          readOnly
          value={value}
          aria-label={label}
          onFocus={(event) => event.target.select()}
          className="min-w-0 flex-1 bg-muted font-mono text-xs"
        />
        <Button
          type="button"
          variant="outline"
          onClick={async () => {
            // No boundary catches a handler. `navigator.clipboard` is
            // absent outside a secure context, so the try covers the
            // property access and not only the rejection.
            try {
              await navigator.clipboard.writeText(value);
              setAttempt({ value, outcome: "copied" });
            } catch {
              setAttempt({ value, outcome: "failed" });
            }
          }}
        >
          {outcome === "copied" ? "Copied" : "Copy"}
        </Button>
      </div>
      {outcome === "failed" && (
        <p role="alert" className="text-sm text-destructive">
          Could not copy the {noun}. Select it in the field and copy it
          yourself.
        </p>
      )}
    </>
  );
}
