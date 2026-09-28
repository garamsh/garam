"use client";

import { useState, type ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

/**
 * A one-time enrollment token, shown where the person can copy it and
 * nowhere else afterwards: garam stores its hash, so nothing can fetch
 * it again (ADR-0064). `children` says when it expires and what the
 * caller's operator does with it.
 *
 * The warning sits above the token rather than below it: someone who
 * has already navigated away has no way back, so it has to be read
 * before the panel is dismissed.
 */
export function EnrollmentToken({
  title,
  token,
  children,
}: {
  title: string;
  token: string;
  children: ReactNode;
}) {
  // Keyed by token: a later answer is a different token, so neither the
  // claim that it was copied nor the failure the previous one reported
  // carries over to it.
  const [attempt, setAttempt] = useState<CopyAttempt | null>(null);
  const outcome = attempt?.token === token ? attempt.outcome : null;

  return (
    <div className="flex max-w-2xl flex-col gap-3 rounded-md border border-border p-4">
      <h3 className="font-heading text-sm font-semibold wrap-anywhere">
        {title}
      </h3>
      <p className="text-sm text-destructive">
        Save this token now. It is shown only once.
      </p>
      <div className="flex gap-2">
        <Input
          readOnly
          value={token}
          aria-label="Enrollment token"
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
              await navigator.clipboard.writeText(token);
              setAttempt({ token, outcome: "copied" });
            } catch {
              setAttempt({ token, outcome: "failed" });
            }
          }}
        >
          {outcome === "copied" ? "Copied" : "Copy"}
        </Button>
      </div>
      {outcome === "failed" && (
        <p role="alert" className="text-sm text-destructive">
          Could not copy the token. Select it in the field and copy it yourself.
        </p>
      )}
      {children}
    </div>
  );
}

/** What the clipboard answered for one enrollment token. */
type CopyAttempt = { token: string; outcome: "copied" | "failed" };

/**
 * The expiry, rendered in the reader's own time zone.
 *
 * It is mounted with `suppressHydrationWarning` because the server
 * renders it in a different zone, and the difference is the point
 * rather than a defect.
 */
export function ExpiresAt({ at }: { at: string }) {
  return (
    <time dateTime={at} suppressHydrationWarning>
      {new Date(at).toLocaleTimeString()}
    </time>
  );
}
