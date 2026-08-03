import Link from "next/link";

import { cn } from "@/lib/utils";

/**
 * The wordmark and its glyph, linking home. Used by the public shell
 * and the sign-in shell so both carry one brand.
 */
export function BrandMark({ className }: { className?: string }) {
  return (
    <Link
      href="/"
      className={cn(
        "inline-flex items-center gap-2.5 rounded-md focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-ring",
        className,
      )}
    >
      <span
        aria-hidden
        className="flex size-6 items-center justify-center rounded-md bg-foreground font-mono text-sm leading-none font-semibold text-background"
      >
        g
      </span>
      <span className="font-heading text-base font-semibold tracking-tight">
        garam
      </span>
    </Link>
  );
}
