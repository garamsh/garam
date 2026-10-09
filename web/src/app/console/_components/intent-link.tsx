"use client";

import Link from "next/link";
import { useState, type ComponentProps } from "react";

/**
 * A console link that prefetches its route once the visitor shows they
 * may follow it — the pointer rests on it, it takes keyboard focus, or a
 * finger touches it — rather than whenever it is on screen. Next.js
 * prefetches every visible link by default, and the sidebar and the
 * breadcrumb put about a dozen of them on every console page, each a
 * server render of a dynamic route nobody asked for (#1209). Intent
 * comes before the click by enough for the prefetch to be under way, so
 * the link the visitor follows is still the one already fetched
 * (Next.js 16.3.1 `docs/01-app/02-guides/prefetching.mdx`, Hover-
 * triggered prefetch).
 */
export function IntentLink({
  onMouseEnter,
  onFocus,
  onTouchStart,
  ...props
}: Omit<ComponentProps<typeof Link>, "prefetch">) {
  const [intent, setIntent] = useState(false);

  return (
    <Link
      {...props}
      prefetch={intent ? null : false}
      onMouseEnter={(event) => {
        setIntent(true);
        onMouseEnter?.(event);
      }}
      onFocus={(event) => {
        setIntent(true);
        onFocus?.(event);
      }}
      onTouchStart={(event) => {
        setIntent(true);
        onTouchStart?.(event);
      }}
    />
  );
}
