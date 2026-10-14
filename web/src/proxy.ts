import { NextResponse, type NextRequest } from "next/server";

import { withReturnPath } from "@/lib/return-path";
import { SESSION_COOKIE } from "@/lib/session";

/**
 * Sends visitors without a session cookie to the login page before the
 * console shell renders. This is a fast path, not the authorization
 * gate: a cookie may be expired or revoked, so `/console` verifies the
 * session against the backend's `/me`, and the login page sends a
 * visitor who already has a valid one on to the console.
 *
 * The address asked for travels to the login page as its return path,
 * so signing in — or registering from an invitation — lands where the
 * visitor was going rather than on the console's front page.
 */
export function proxy(request: NextRequest) {
  if (request.cookies.has(SESSION_COOKIE)) {
    return NextResponse.next();
  }
  const { pathname, search } = request.nextUrl;
  return NextResponse.redirect(
    new URL(withReturnPath("/login", pathname + search), request.url),
  );
}

/** The routes `proxy` runs on; Next.js reads this export by name. */
export const config = {
  // Everything outside the console — the landing page, `/login`, static
  // assets, and the `/api` rewrite the OAuth round trip runs through —
  // is public.
  matcher: "/console/:path*",
};
