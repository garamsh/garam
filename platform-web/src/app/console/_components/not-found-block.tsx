import Link from "next/link";

import { Button } from "@garam/design/ui/button";

import { MarkMissing } from "./missing-page";

/**
 * What a console page that does not exist shows, on the page grid like
 * any other page. An organization or entry the caller cannot see
 * answers the same as one that does not exist (ADR-0011), so the copy
 * names both.
 */
export function NotFoundBlock() {
  return (
    <section
      aria-labelledby="console-not-found-title"
      className="flex max-w-lg flex-col items-start gap-4"
    >
      <MarkMissing />
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
