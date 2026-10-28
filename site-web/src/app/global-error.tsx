"use client";

import { Button } from "@garam/design/ui/button";

/**
 * Last-resort boundary: it replaces the root layout when the error is
 * above it, so it renders its own `<html>` and `<body>`
 * (stack-nextjs.md §App Router file conventions).
 */
export default function GlobalError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return (
    <html lang="en">
      <body>
        <main className="flex min-h-screen flex-col items-start justify-center gap-4 p-8">
          <div className="flex flex-col gap-1">
            <h1 className="font-heading text-2xl font-semibold">
              Something went wrong
            </h1>
            <p className="text-sm text-muted-foreground">{error.message}</p>
          </div>
          <Button onClick={reset} variant="outline">
            Try again
          </Button>
        </main>
      </body>
    </html>
  );
}
