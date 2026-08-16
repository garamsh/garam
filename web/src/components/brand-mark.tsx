import Link from "next/link";

import { cn } from "@/lib/utils";

/**
 * The mark and the wordmark, linking home. Used by the public shell
 * and the sign-in shell so both carry one brand.
 *
 * The link's font-size is the kit's S — the icon height — so the lockup
 * ratios hold at whatever size a caller sets: gap 0.28 S, wordmark
 * 0.7143 S, whose 0.70em cap height is the kit's 0.50 S.
 */
export function BrandMark({ className }: { className?: string }) {
  return (
    <Link
      href="/"
      className={cn(
        "inline-flex items-center gap-[0.28em] rounded-md text-2xl focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-ring",
        className,
      )}
    >
      {/* Plate and glyph are one path; the even-odd rule knocks the glyph out. */}
      <svg viewBox="0 0 100 100" aria-hidden className="size-[1em]">
        <path
          fill="currentColor"
          fillRule="evenodd"
          d="M4 0H96A4 4 0 0 1 100 4V96A4 4 0 0 1 96 100H4A4 4 0 0 1 0 96V4A4 4 0 0 1 4 0ZM17 15H45V29H17ZM45 15H59V85H45ZM17 71H45V85H17ZM59 43H87V57H59Z"
        />
      </svg>
      {/* Poppins' ascent less its descent is its cap height, so the
          centred line box centres the caps on the icon. */}
      <span className="font-heading text-[0.7143em] leading-none font-semibold tracking-[0.02em]">
        garam
      </span>
    </Link>
  );
}
