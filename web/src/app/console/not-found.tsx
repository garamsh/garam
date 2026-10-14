import Link from "next/link";

import { Button } from "@/components/ui/button";

/**
 * What `notFound()` renders anywhere in the console, inside the shell
 * so the sidebar and the palette stay to hand. An organization the
 * caller cannot see answers the same as one that does not exist
 * (ADR-0011), so the copy names both.
 */
export default function ConsoleNotFound() {
  return (
    <section
      aria-labelledby="console-not-found-title"
      className="mx-auto flex w-full max-w-md flex-col items-start gap-4 py-16"
    >
      <p className="font-mono text-xs text-muted-foreground">404</p>
      <div className="flex flex-col gap-1.5">
        <h1 id="console-not-found-title" className="text-xl tracking-tight">
          Page not found
        </h1>
        <p className="text-muted-foreground">
          This page doesn’t exist, or you don’t have access to it. Check the
          address, or start again from your organizations.
        </p>
      </div>
      <Button asChild variant="outline">
        <Link href="/console/orgs">Your organizations</Link>
      </Button>
    </section>
  );
}
