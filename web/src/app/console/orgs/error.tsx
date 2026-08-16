"use client";

import { Button } from "@/components/ui/button";

/**
 * Error boundary for the organizations subtree. Client Component by
 * framework contract (stack-nextjs.md §2).
 */
export default function OrgsError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return (
    <section role="alert" className="flex flex-col items-start gap-4">
      <div className="flex flex-col gap-1">
        <h1 className="font-heading text-2xl font-semibold">
          Something went wrong
        </h1>
        <p className="text-sm text-muted-foreground">{error.message}</p>
      </div>
      <Button onClick={reset} variant="outline">
        Try again
      </Button>
    </section>
  );
}
