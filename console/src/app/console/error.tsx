"use client";

import Link from "next/link";
import { RotateCw } from "lucide-react";

import { Button } from "@/components/ui/button";

/**
 * The console's error boundary: a page below the shell failed while it
 * rendered, and the shell stays standing around this. It says what
 * happened in words and never the thrown message, which is for the
 * server log; the digest is what matches that log, so it is offered as
 * a reference (stack-nextjs.md §Errors — returned, thrown, or caught).
 */
export default function ConsoleError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return (
    <section
      role="alert"
      aria-labelledby="console-error-title"
      className="flex max-w-lg flex-col items-start gap-4"
    >
      <div className="flex flex-col gap-1.5">
        <h1 id="console-error-title" className="text-xl tracking-tight">
          This page didn’t load
        </h1>
        <p className="text-muted-foreground">
          Something went wrong on our side while loading it. Try again, and if
          it keeps happening, share the reference below with your administrator.
        </p>
      </div>
      {error.digest && (
        <p className="text-muted-foreground">
          Reference{" "}
          <code className="rounded-md bg-muted px-1.5 py-0.5 font-mono text-xs text-foreground">
            {error.digest}
          </code>
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        <Button type="button" onClick={reset}>
          <RotateCw />
          Try again
        </Button>
        <Button asChild variant="outline">
          <Link href="/console/orgs">Your organizations</Link>
        </Button>
      </div>
    </section>
  );
}
