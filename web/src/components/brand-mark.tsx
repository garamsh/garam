import Link from "next/link";

import { cn } from "@/lib/utils";

/**
 * The mark and the wordmark, linking home. Used by the public shell
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
      {/* Plate and glyph are one path; the even-odd rule knocks the glyph out. */}
      <svg viewBox="0 0 100 100" aria-hidden className="size-6">
        <path
          fill="currentColor"
          fillRule="evenodd"
          d="M4 0H96A4 4 0 0 1 100 4V96A4 4 0 0 1 96 100H4A4 4 0 0 1 0 96V4A4 4 0 0 1 4 0ZM17 15H45V29H17ZM45 15H59V85H45ZM17 71H45V85H17ZM59 43H87V57H59Z"
        />
      </svg>
      <span className="font-heading text-base font-semibold tracking-tight">
        garam
      </span>
    </Link>
  );
}
